package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"gnosis"
	"gnosis/internal/assetform"
)

func (a *desktop) view(c *ui.Context) {
	ui.Column(c).Fill().Padding(20).Gap(14).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Text(c, "Gnosis").FontSize(24).Bold().Grow(1)
			a.tab(c, "edit", "用例与套件")
			a.tab(c, "run", "执行")
			a.tab(c, "model", "模型设置")
		})
		if a.problem != "" {
			ui.Text(c, a.problem).Wrap().TextColor(ui.Hex("#b44632"))
		} else if a.message != "" {
			ui.Text(c, a.message).Wrap()
		}
		switch a.page {
		case "run":
			a.runView(c)
		case "model":
			a.modelView(c)
		default:
			a.editorView(c)
		}
	})
}

func (a *desktop) tab(c *ui.Context, page, label string) {
	if a.page == page {
		ui.PrimaryButton(c, label)
		return
	}
	if ui.Button(c, label).Clicked() {
		a.page, a.message = page, ""
		if page == "run" {
			a.refreshDevices()
		}
	}
}

func (a *desktop) editorView(c *ui.Context) {
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, "打开 YAML").Clicked() {
			a.pickAsset()
		}
		if ui.Button(c, "新建用例").Clicked() {
			a.newAsset(false)
		}
		if ui.Button(c, "新建套件").Clicked() {
			a.newAsset(true)
		}
		if a.doc != nil {
			if ui.Button(c, "保存").Clicked() {
				a.saveAsset(false)
			}
			if ui.Button(c, "另存为").Clicked() {
				a.saveAsset(true)
			}
			if ui.Button(c, "重新载入").Disabled(a.doc.Path() == "").Clicked() && a.canDiscard() {
				a.loadAsset(a.doc.Path())
			}
		}
	})
	if a.doc == nil {
		ui.Text(c, "打开现有用例或套件，或创建一个新文件。").Wrap()
		return
	}
	label := "未命名用例"
	if a.doc.Suite() {
		label = "未命名套件"
	}
	if a.doc.Path() != "" {
		label = a.doc.Path()
	}
	if a.doc.Dirty() {
		label += "  ·  未保存"
	}
	ui.Text(c, label).Wrap()
	if !a.doc.Suite() {
		ui.Row(c).Gap(8).Children(func() {
			name := a.doc.Name()
			if ui.TextInput(c.Key("case-name"), &name).Label("用例名称").Placeholder("用例名称").Width(360).Changed() {
				a.doc.SetName(name)
			}
			if a.doc.HasName() && ui.Button(c, "使用文件名").Clicked() {
				a.doc.ClearName()
			} else if !a.doc.HasName() && ui.Button(c, "设为空名称").Clicked() {
				a.doc.SetName("")
			}
		})
	}
	ui.Row(c).Grow(1).Gap(16).Children(func() {
		ui.Column(c).Width(300).MinHeight(0).Gap(8).Children(func() { a.editRows(c) })
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			if a.row >= 0 && a.row < a.doc.Rows() && !a.doc.Suite() {
				a.stepFields(c)
			}
		})
	})
}

func (a *desktop) editRows(c *ui.Context) {
	label := "添加步骤"
	if a.doc.Suite() {
		label = "添加用例"
	}
	if ui.Button(c, label).Clicked() {
		if a.doc.Suite() {
			a.addReferences()
		} else {
			a.doc.AddStep()
			a.row = a.doc.Rows() - 1
		}
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(5).FillWidth().Children(func() {
			for i := 0; i < a.doc.Rows(); i++ {
				text := a.doc.Reference(i)
				if !a.doc.Suite() {
					text, _ = a.doc.Field(i, assetform.StepName)
					if text == "" {
						text = stepLabel(a.doc.StepKind(i))
					}
				}
				text = fmt.Sprintf("%d  %s", i+1, text)
				if a.row == i {
					ui.PrimaryButton(c.Key(i), text).FillWidth()
				} else if ui.Button(c.Key(i), text).FillWidth().Clicked() {
					a.row = i
				}
			}
		})
	})
	ui.Row(c).Gap(6).Children(func() {
		if ui.Button(c, "上移").Disabled(a.row <= 0).Clicked() {
			a.doc.MoveRow(a.row, -1)
			a.row--
		}
		if ui.Button(c, "下移").Disabled(a.row < 0 || a.row >= a.doc.Rows()-1).Clicked() {
			a.doc.MoveRow(a.row, 1)
			a.row++
		}
		if ui.Button(c, "移除").Disabled(a.row < 0).Clicked() {
			a.doc.RemoveRow(a.row)
			a.row = min(a.row, a.doc.Rows()-1)
		}
	})
}

func (a *desktop) stepFields(c *ui.Context) {
	row := a.row
	ui.Column(c.Key(row)).FillWidth().Padding(0, 8).Gap(9).Children(func() {
		kind := a.doc.StepKind(row)
		selected := stepLabel(kind)
		kinds := make([]string, len(assetform.StepKinds))
		for i, value := range assetform.StepKinds {
			kinds[i] = stepLabel(value)
		}
		ui.Text(c, "步骤类型").Bold()
		ui.Select(c.Key("kind"), &selected, kinds).Label("步骤类型").Width(235)
		if index := slices.Index(kinds, selected); index >= 0 && assetform.StepKinds[index] != kind {
			kind = assetform.StepKinds[index]
			a.doc.SetStepKind(row, kind)
		}
		check := a.doc.CheckKind(row)
		if kind == "precondition" || kind == "expect" {
			ui.Text(c, "检查方式").Bold()
			next := check
			ui.Select(c.Key("check"), &next, assetform.CheckKinds).Label("检查方式").Width(235)
			if next != check {
				check = next
				a.doc.SetCheckKind(row, check)
			}
		}
		for _, field := range assetform.Fields(kind, check) {
			a.renderField(c, row, kind, check, field)
		}
	})
}

func (a *desktop) renderField(c *ui.Context, row int, kind, check string, field assetform.Field) {
	value, present := a.doc.Field(row, field)
	next := value
	ui.Text(c, field.Label)
	key := kind + ":" + check + ":" + field.Label
	ui.Row(c.Key(key)).Gap(8).Children(func() {
		if len(field.Choices) > 0 {
			ui.Select(c, &next, field.Choices).Label(field.Label).Width(235)
		} else {
			ui.TextInput(c, &next).Label(field.Label).Placeholder("未设置").Width(340)
		}
		if next != value {
			a.doc.SetField(row, field, next)
		}
		if field.Optional && present && ui.Button(c, "省略").Clicked() {
			a.doc.ClearField(row, field)
		}
		if !present && next == "" && len(field.Choices) == 0 && ui.Button(c, "设为空").Clicked() {
			a.doc.SetField(row, field, "")
		}
	})
}

func (a *desktop) runView(c *ui.Context) {
	ui.Row(c).Gap(8).Children(func() {
		label := "正在连接本机执行进程"
		if a.connected {
			label = "本机执行进程已连接"
		}
		ui.Text(c, label).Grow(1)
		if ui.Button(c, "刷新设备").Disabled(!a.connected || a.refreshing).Clicked() {
			a.refreshDevices()
		}
	})
	if a.connectionError != "" {
		ui.Text(c, a.connectionError).Wrap()
	}
	if a.deviceError != "" {
		ui.Text(c, a.deviceError).Wrap()
	} else if len(a.devices) == 0 && a.connected && !a.refreshing {
		ui.Text(c, "未发现安卓设备。连接手机并允许 USB 调试后刷新设备。").Wrap()
	}
	if a.deviceID != "" && !a.selectedOnline() {
		ui.Text(c, "选定设备不可用 · "+a.deviceID).Wrap()
	}
	for _, device := range a.devices {
		parts := []string{device.Platform, device.ID}
		if device.Name != "" {
			parts = append(parts, device.Name)
		}
		if device.State != "device" {
			parts = append(parts, device.State)
		}
		label := strings.Join(parts, " · ")
		if device.ID == a.deviceID && a.selectedOnline() {
			ui.PrimaryButton(c.Key(device.ID), "已选择 · "+label)
		} else if ui.Button(c.Key(device.ID), label).Disabled(device.State != "device").Clicked() {
			a.deviceID = device.ID
		}
	}
	if a.doc == nil {
		ui.Text(c, "请在用例与套件页打开或创建文件。")
	} else if path := a.doc.Path(); path != "" {
		ui.Text(c, "待执行文件 · "+filepath.Base(path)).Wrap()
	} else {
		ui.Text(c, "待执行文件 · 未保存")
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, "开始执行").Disabled(a.doc == nil || !a.connected || !a.selectedOnline() || a.commandBusy || a.snapshot.Active()).Clicked() {
			a.start()
		}
		if a.pending != "" && ui.Button(c, "核对启动").Disabled(a.commandBusy || !a.connected).Clicked() {
			a.checkRequest()
		}
	})
	a.progressView(c)
}

func (a *desktop) progressView(c *ui.Context) {
	if a.snapshot.ID == "" {
		return
	}
	p := a.snapshot.Progress
	ui.Text(c, stateLabel(p.State)+" · "+filepath.Base(a.snapshot.Request.Path)+" · "+a.snapshot.Request.DeviceID).Bold().Wrap()
	if p.Case > 0 && a.snapshot.Active() {
		label := fmt.Sprintf("用例 %d", p.Case)
		if p.Step > 0 {
			label += fmt.Sprintf(" · 步骤 %d", p.Step)
		}
		if p.StepName != "" {
			label += " · " + p.StepName
		}
		if p.Turn > 0 {
			label += fmt.Sprintf(" · AI 轮次 %d", p.Turn)
		}
		ui.Text(c, label).Wrap()
	}
	if p.Diagnostic != "" {
		ui.Text(c, p.Diagnostic).Wrap()
	}
	if a.snapshot.Active() {
		ui.Row(c).Gap(8).Children(func() {
			unavailable := !a.connected || a.commandBusy
			if ui.Button(c, "暂停").Disabled(unavailable || p.State != gnosis.ExecutionRunning).Clicked() {
				a.control("pause")
			}
			if ui.Button(c, "恢复").Disabled(unavailable || (p.State != gnosis.ExecutionPaused && p.State != gnosis.ExecutionPausing)).Clicked() {
				a.control("resume")
			}
			if ui.Button(c, "取消执行").Disabled(unavailable || p.State == gnosis.ExecutionCancelling || p.State == gnosis.ExecutionFinishing).Clicked() {
				a.control("cancel")
			}
		})
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(9).Children(func() {
			for i, item := range p.Cases {
				ui.Row(c.Key(i)).Gap(8).FillWidth().Children(func() {
					label := stateLabel(item.State)
					if item.Result != "" {
						label = resultLabel(item.Result)
					}
					ui.Text(c, fmt.Sprintf("%d  %s · %s", i+1, item.Name, label)).Wrap().Grow(1)
					if item.ReportPath != "" && ui.Button(c, "打开报告").Clicked() {
						a.openReport(item.ReportPath)
					}
				})
				if item.Diagnostic != "" {
					ui.Text(c, item.Diagnostic).Wrap()
				}
			}
		})
	})
}

func (a *desktop) modelView(c *ui.Context) {
	ui.Text(c, "模型地址").Bold()
	ui.TextInput(c.Key("endpoint"), &a.model.Endpoint).Label("模型地址").Placeholder("完整的 Chat Completions URL").MaxWidth(580)
	ui.Text(c, "模型名称").Bold()
	ui.TextInput(c.Key("model-name"), &a.model.Name).Label("模型名称").MaxWidth(380)
	label := "API 密钥"
	if a.model.HasKey {
		label += " · 已存入 Windows 凭据管理器"
	}
	ui.Text(c, label).Bold()
	ui.TextInput(c.Key("model-key"), &a.secret).Label("API 密钥").Password().Placeholder("留空保留原有密钥").MaxWidth(380)
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, "保存模型设置").Clicked() {
			a.storeModel(false)
		}
		if a.model.HasKey && ui.Button(c, "删除密钥").Clicked() && a.confirm("删除模型密钥", "下一次执行 AI 步骤前需要重新设置密钥。", "删除密钥") {
			a.storeModel(true)
		}
	})
}

func stepLabel(v string) string {
	switch v {
	case "launch":
		return "打开应用"
	case "tap":
		return "点击元素"
	case "long_press":
		return "长按元素"
	case "tap_at":
		return "点击坐标"
	case "swipe":
		return "滑动"
	case "type":
		return "替换文本"
	case "key":
		return "按键"
	case "wait":
		return "等待"
	case "precondition":
		return "前置断言"
	case "expect":
		return "预期断言"
	case "ai":
		return "AI 步骤"
	default:
		return "选择步骤类型"
	}
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
