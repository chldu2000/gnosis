package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"gnosis"
	"gnosis/internal/localrun"
)

func (a *desktop) connectionLabel() string {
	switch {
	case a.connected:
		return "本机执行进程已连接"
	case a.connecting && a.connectionError != "":
		return "正在重连本机执行进程"
	case a.connectionError != "":
		return "本机执行连接中断"
	default:
		return "正在连接本机执行进程"
	}
}

func (a *desktop) runView(c *ui.Context) {
	running := a.snapshot.Active()
	preparing := a.connected && !running && (a.pending == nil || a.pending.checked)
	if preparing {
		a.prepareView(c.Key("prepare"))
	}
	if !a.connected {
		a.connectionView(c.Key("connection"))
	}
	if a.runProblem != "" {
		ui.Text(c.Key("execution-error"), a.runProblem).Wrap().TextColor(c.Theme().Danger)
	}
	if !running && a.pending != nil {
		if preparing || !a.connected {
			ui.Divider(c.Key("pending-divider"))
		}
		a.pendingView(c.Key("pending"))
	}
	if a.snapshot.ID != "" {
		if preparing || !a.connected || a.pending != nil {
			ui.Divider(c.Key("result-divider"))
		}
		a.progressView(c.Key(a.snapshot.ID))
	}
	if running {
		if a.pending != nil {
			ui.Divider(c.Key("pending-divider"))
			a.pendingView(c.Key("pending"))
		}
		if a.doc != nil && (a.doc.Dirty() || !strings.EqualFold(a.doc.Path(), a.snapshot.Request.Path)) {
			ui.Divider(c.Key("editor-divider"))
			ui.Text(c.Key("editing-heading"), "编辑中的文件").Bold()
			ui.Text(c.Key("editing-file"), documentTitle(a.doc)).FontSize(12).Wrap().TextColor(c.Theme().TextMuted)
		}
	}
}

func (a *desktop) connectionView(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(8).Gap(5).Border(1, c.Theme().Border).Radius(5).Children(func() {
		ui.Text(c, a.connectionLabel()).Bold().Wrap()
		if a.snapshot.ID != "" {
			ui.Text(c, "下方是上次同步的状态。执行可能仍在继续，连接恢复后可核对进度。").FontSize(12).Wrap()
		}
		if a.connectionError != "" {
			ui.Text(c, "连接诊断").FontSize(12).TextColor(c.Theme().TextMuted).Tooltip(a.connectionError)
			if ui.Button(c, "立即重连").Disabled(a.connecting).Clicked() {
				select {
				case a.retry <- struct{}{}:
				default:
				}
			}
		}
	})
}

func deviceName(device gnosis.DeviceInfo) string {
	parts := make([]string, 0, 2)
	if device.Name != "" {
		parts = append(parts, device.Name)
	}
	if device.Platform != "" {
		parts = append(parts, device.Platform)
	}
	return strings.Join(parts, " · ")
}

func (a *desktop) selectedDeviceLabel() string {
	if a.deviceName == "" {
		return a.deviceID
	}
	return a.deviceName + " · " + a.deviceID
}

func (a *desktop) unavailableDeviceLabel() string {
	for _, device := range a.devices {
		if device.ID == a.deviceID && device.State == "unauthorized" {
			return a.selectedDeviceLabel() + " 未授权"
		}
	}
	return a.selectedDeviceLabel() + " 已离线"
}

func (a *desktop) prepareView(c *ui.Context) {
	title, action := "准备执行", "开始执行"
	if a.pending != nil {
		title, action = "准备新执行", "发起新执行…"
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, title).FontSize(17).Bold()
		switch {
		case a.doc == nil:
			ui.Text(c, "打开或新建一个用例或套件。").FontSize(12).Wrap()
		case a.doc.Suite() && a.doc.Rows() == 0:
			ui.Text(c, "先为套件添加用例。").FontSize(12).Wrap()
		default:
			label := "待执行 · " + documentTitle(a.doc)
			if a.doc.Dirty() || a.doc.Path() == "" {
				label += " · 未保存"
			}
			ui.Text(c, label).FontSize(12).Wrap()
		}
		a.devicePicker(c)
		var button ui.Element
		if a.pending == nil {
			button = ui.PrimaryButton(c, action)
		} else {
			button = ui.Button(c, action)
		}
		if button.FillWidth().Disabled(!a.canStart()).Clicked() {
			a.start()
		}
	})
}

func (a *desktop) devicePicker(c *ui.Context) {
	ui.Row(c.Key("device-heading")).AlignItems(ui.Center).Gap(5).Children(func() {
		ui.Text(c, "设备").Bold().Grow(1)
		label := "刷新设备"
		if a.refreshing {
			label = "正在刷新"
		}
		if ui.Button(c, label).Disabled(a.refreshing || a.commandBusy).Clicked() {
			a.refreshDevices()
		}
	})
	options := []string{"选择在线设备"}
	online := []gnosis.DeviceInfo{{}}
	selected := options[0]
	for _, device := range a.devices {
		if device.State != "device" {
			continue
		}
		name := deviceName(device)
		label := device.ID
		if name != "" {
			label = name + " · " + device.ID
		}
		options, online = append(options, label), append(online, device)
		if device.ID == a.deviceID {
			selected = label
		}
	}
	if ui.Select(c.Key("device-picker"), &selected, options).Label("设备").FillWidth().
		Disabled(a.refreshing || a.commandBusy || a.deviceError != "").Changed() {
		for i, option := range options {
			if option == selected {
				a.deviceID, a.deviceName = online[i].ID, deviceName(online[i])
				break
			}
		}
	}
	theme := c.Theme()
	switch {
	case a.deviceError != "":
		ui.Text(c, a.deviceError).FontSize(12).Wrap().TextColor(theme.Danger)
	case !a.refreshing && a.deviceID != "" && !a.selectedOnline():
		ui.Text(c, a.unavailableDeviceLabel()).FontSize(12).Bold().Wrap().TextColor(theme.Warning)
		ui.Text(c, "重新连接设备后刷新，或选择另一台在线设备。").FontSize(12).Wrap()
	case !a.refreshing && len(online) == 1:
		ui.Text(c, "未发现在线设备。连接设备并授权调试后刷新。").FontSize(12).Wrap()
	case a.deviceID != "":
		ui.Text(c, "设备标识 · "+a.deviceID).FontSize(12).Wrap().TextColor(theme.TextMuted)
	}
}

func (a *desktop) pendingView(c *ui.Context) {
	pending := a.pending
	ui.Column(c).FillWidth().Padding(7).Gap(5).Border(1, c.Theme().Warning).Radius(4).Children(func() {
		ui.Text(c, "启动待确认").FontSize(15).Bold()
		ui.Text(c, pending.title+" · "+pending.request.DeviceID).FontSize(12).Wrap()
		ui.Text(c, pending.request.Path).FontSize(12).Wrap().TextColor(c.Theme().TextMuted)
		status, action := "尚未收到启动回执。核对原请求后可接入该次执行。", "核对启动"
		switch {
		case pending.checking:
			status, action = "正在查找这次启动请求。", "正在核对…"
		case pending.checkNote != "":
			status = pending.checkNote
		case pending.checked:
			status = "暂未找到匹配的执行。可以再次核对，或明确发起一次新执行。"
		}
		ui.Text(c, status).FontSize(12).Wrap()
		if ui.PrimaryButton(c, action).Disabled(a.commandBusy || !a.connected).Clicked() {
			a.checkRequest()
		}
	})
}

func (a *desktop) activeCaseSource() (gnosis.CaseSource, bool) {
	s := a.snapshot
	index := s.Progress.Case - 1
	if !s.Active() || !a.connected || index < 0 || index >= len(s.CaseSources) {
		return gnosis.CaseSource{}, false
	}
	return s.CaseSources[index], true
}

func (a *desktop) liveStep() int {
	source, available := a.activeCaseSource()
	row := a.snapshot.Progress.Step - 1
	if !available || a.doc == nil || !strings.EqualFold(a.doc.Path(), source.Path) ||
		!a.doc.MatchesSavedVersion(source.Digest) || row < 0 || row >= a.doc.Rows() {
		return -1
	}
	return row
}

func (a *desktop) openActiveCase(path string) {
	doc := a.loadAsset(path)
	if doc == nil {
		return
	}
	if row := a.liveStep(); row >= 0 {
		doc.row, doc.focusRow = row, true
	} else if source, ok := a.activeCaseSource(); ok && !doc.MatchesSavedVersion(source.Digest) {
		a.message = "当前内容与本次执行载入的版本不同。"
	}
}

func (a *desktop) progressView(c *ui.Context) {
	s := a.snapshot
	p := s.Progress
	heading := "最近一次执行"
	if s.Active() {
		heading = "当前执行"
	}
	theme := c.Theme()
	ui.Text(c.Key("run-heading"), heading).FontSize(16).Bold()
	ui.Column(c.Key("summary")).FillWidth().Padding(7).Gap(4).
		Background(theme.Background).Border(1, theme.Border).Radius(4).Children(func() {
		if !a.connected {
			ui.Text(c, "上次同步").FontSize(12).TextColor(theme.Warning)
		}
		name := filepath.Base(s.Request.Path)
		if !s.Suite && len(p.Cases) > 0 && p.Cases[0].Name != "" {
			name = p.Cases[0].Name
		}
		ui.Text(c, name).Bold().Wrap()
		ui.Text(c, stateLabel(p.State)+" · "+s.Request.DeviceID).FontSize(12).Wrap()
		if !s.Active() {
			if p.State == gnosis.ExecutionInterrupted {
				ui.Text(c, "原执行进程已退出。已保存的报告仍可查看。").FontSize(12).Wrap()
			}
			return
		}
		if label := positionLabel(s); label != "" {
			ui.Text(c, label).FontSize(12).Wrap()
		}
		if index := p.Case - 1; index >= 0 && index < len(s.CaseSources) && s.CaseSources[index].Steps > 0 {
			count := s.CaseSources[index].Steps
			ui.Progress(c, float64(min(max(0, p.Step-1), count))/float64(count)).FillWidth()
		}
		if a.connected && p.State == gnosis.ExecutionPausing {
			ui.Text(c, "到安全点后暂停。").FontSize(12).Wrap()
		} else if a.connected && p.State == gnosis.ExecutionPaused {
			ui.Text(c, "设备仍由本次执行占用。").FontSize(12).Wrap()
		} else if a.connected && (p.State == gnosis.ExecutionCancelling || p.State == gnosis.ExecutionFinishing) {
			ui.Text(c, "正在保存记录并释放设备。").FontSize(12).Wrap()
		}
	})
	if p.Diagnostic != "" {
		ui.Text(c.Key("diagnostic"), p.Diagnostic).FontSize(12).Wrap()
	}
	if s.Active() {
		a.executionControls(c.Key("commands"))
	}
	if p.ReportPath != "" && ui.Button(c.Key("session-report"), "打开会话报告").Clicked() {
		a.openReport(p.ReportPath)
	}
	a.resultsView(c.Key("results"))
}

func positionLabel(s localrun.Snapshot) string {
	p := s.Progress
	if p.Case <= 0 {
		return ""
	}
	index := p.Case - 1
	var parts []string
	if s.Suite {
		title := fmt.Sprintf("用例 %d", p.Case)
		if len(s.CaseSources) > index {
			title += fmt.Sprintf("/%d", len(s.CaseSources))
		}
		if len(p.Cases) > index {
			title += " · " + p.Cases[index].Name
		}
		parts = append(parts, title)
	}
	if p.Step > 0 {
		step := fmt.Sprintf("步骤 %d", p.Step)
		if len(s.CaseSources) > index && s.CaseSources[index].Steps > 0 {
			step += fmt.Sprintf("/%d", s.CaseSources[index].Steps)
		}
		if p.StepName != "" {
			step += " · " + p.StepName
		}
		parts = append(parts, step)
	}
	if p.Turn > 0 {
		parts = append(parts, fmt.Sprintf("AI 轮次 %d", p.Turn))
	}
	return strings.Join(parts, " · ")
}

func (a *desktop) executionControls(c *ui.Context) {
	id := a.snapshot.ID
	state := a.snapshot.Progress.State
	unavailable := !a.connected || a.commandBusy
	ui.Row(c).Gap(5).Children(func() {
		if state == gnosis.ExecutionPaused || state == gnosis.ExecutionPausing {
			if ui.PrimaryButton(c.Key("resume"), "继续").Disabled(unavailable).Clicked() {
				a.control("resume")
			}
		} else if ui.Button(c.Key("pause"), "暂停").Disabled(unavailable || state != gnosis.ExecutionRunning).Clicked() {
			a.control("pause")
		}
		if ui.Button(c.Key("cancel"), "取消执行").Disabled(unavailable || state == gnosis.ExecutionCancelling ||
			state == gnosis.ExecutionFinishing).Clicked() &&
			a.confirm("取消本次执行？", "会保留已完成的步骤和已保存的报告。", "确认取消") && a.snapshot.ID == id {
			a.control("cancel")
		}
	})
	if source, ok := a.activeCaseSource(); ok {
		row := a.liveStep()
		switch {
		case row >= 0:
			if ui.Button(c.Key("locate"), "定位步骤").Clicked() {
				a.doc.row, a.doc.focusRow = row, true
			}
		case a.doc == nil || !strings.EqualFold(a.doc.Path(), source.Path):
			if ui.Button(c.Key("open"), "打开当前用例").Clicked() {
				a.openActiveCase(source.Path)
			}
		case !a.doc.MatchesSavedVersion(source.Digest):
			ui.Text(c.Key("changed"), "当前内容与本次执行载入的版本不同。").FontSize(12).Wrap().TextColor(c.Theme().TextMuted)
		}
	}
}

func (a *desktop) resultsView(c *ui.Context) {
	s := a.snapshot
	ui.Text(c.Key("heading"), "用例结果").Bold()
	ui.Scroll(c.Key("list")).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for i := 0; i < max(len(s.Progress.Cases), len(s.CaseSources)); i++ {
				item := gnosis.CaseProgress{}
				started := i < len(s.Progress.Cases)
				if started {
					item = s.Progress.Cases[i]
				} else {
					item.Name = s.CaseSources[i].Name
				}
				ui.Column(c.Key(i)).FillWidth().Gap(4).Children(func() {
					ui.Divider(c)
					name := item.Name
					if s.Suite {
						name = fmt.Sprintf("%d. %s", i+1, name)
					}
					ui.Text(c, name).Bold().Wrap()
					label := "未开始"
					if started {
						label = stateLabel(item.State)
					}
					if item.Result != "" {
						label = resultLabel(item.Result)
					}
					color := c.Theme().TextMuted
					switch item.Result {
					case gnosis.ResultPass:
						color = c.Theme().Success
					case gnosis.ResultFail, gnosis.ResultError:
						color = c.Theme().Danger
					case gnosis.ResultBlocked:
						color = c.Theme().Warning
					}
					ui.Text(c, label).FontSize(12).Wrap().TextColor(color)
					if diagnostic := caseDiagnostic(item); diagnostic != "" {
						ui.Text(c, diagnostic).FontSize(12).Wrap()
					}
					if s.Progress.ReportPath == "" && item.ReportPath != "" {
						action := "打开报告"
						if item.State == gnosis.ExecutionCancelled {
							action = "打开部分报告"
						}
						if ui.Button(c, action).Clicked() {
							a.openReport(item.ReportPath)
						}
					}
				})
			}
		})
	})
}

func caseDiagnostic(item gnosis.CaseProgress) string {
	if item.Diagnostic != "" {
		return item.Diagnostic
	}
	var reason string
	switch item.Result {
	case gnosis.ResultFail:
		reason = "预期断言未成立"
	case gnosis.ResultBlocked:
		reason = "前置断言未成立"
	default:
		return ""
	}
	if item.FailedStep <= 0 {
		return reason
	}
	step := fmt.Sprintf("步骤 %d", item.FailedStep)
	if item.StepName != "" {
		step += " · " + item.StepName
	}
	return step + " · " + reason
}

func stateLabel(v gnosis.ExecutionState) string {
	switch v {
	case gnosis.ExecutionRunning:
		return "执行中"
	case gnosis.ExecutionPausing:
		return "正在暂停"
	case gnosis.ExecutionPaused:
		return "已暂停"
	case gnosis.ExecutionCancelling:
		return "正在取消"
	case gnosis.ExecutionFinishing:
		return "正在收尾"
	case gnosis.ExecutionCancelled:
		return "已取消"
	case gnosis.ExecutionFinished:
		return "已结束"
	case gnosis.ExecutionInterrupted:
		return "执行中断"
	default:
		return string(v)
	}
}

func resultLabel(v gnosis.Result) string {
	switch v {
	case gnosis.ResultPass:
		return "通过"
	case gnosis.ResultFail:
		return "失败"
	case gnosis.ResultBlocked:
		return "阻塞"
	case gnosis.ResultError:
		return "错误"
	case gnosis.ResultExecuted:
		return "已执行"
	default:
		return string(v)
	}
}
