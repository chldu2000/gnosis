package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gnosis"
	"gnosis/internal/localrun"
)

const windowTitle = "Gnosis"

type desktop struct {
	picker       elementPicker
	captureEpoch uint64
	evidenceOpen bool
	evidence     ui.ImageSource
	window       *mygo.Window
	client       *localrun.Client
	ctx          context.Context
	width        int
	retry        chan struct{}

	doc       *openedDocument
	documents []*openedDocument
	nextDocID int
	filesOpen bool
	modelOpen bool

	snapshot        localrun.Snapshot
	connected       bool
	connecting      bool
	connectionError string
	connectionEpoch uint64
	devices         []gnosis.DeviceInfo
	deviceID        string
	deviceName      string
	deviceError     string
	refreshing      bool
	commandBusy     bool
	pending         *pendingStart
	autoOpen        string
	runProblem      string

	model        localrun.ModelSummary
	secret       string
	modelNote    string
	modelProblem string
	message      string
	problem      string
}

type pendingStart struct {
	request   localrun.StartRequest
	title     string
	checked   bool
	checking  bool
	checkNote string
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--execution-host" {
		if err := localrun.Serve(); err != nil {
			log.Fatal(err)
		}
		return
	}
	client, err := localrun.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer client.Close()
	app := &desktop{ctx: ctx, client: client, width: 1040, retry: make(chan struct{}, 1)}
	mygo.App.WhenReady(func() {
		app.window = mygo.NewWindow(mygo.WindowOptions{
			Title: windowTitle, Width: 1040, Height: 740, MinWidth: 760, MinHeight: 540,
			Content: ui.View(app.view),
		})
		app.window.OnClose(func(event *mygo.CloseEvent) {
			if !app.canCloseWindow() {
				event.PreventDefault()
			}
		})
		app.window.OnResize(func() {
			width, _ := app.window.ContentSize()
			if width > 0 && width != app.width {
				app.window.Update(func() { app.width = width })
			}
		})
		app.window.OnClosed(cancel)
		if len(os.Args) == 2 {
			app.loadAsset(os.Args[1])
		}
		go app.observe()
		go func() {
			settings, err := localrun.ModelSettings()
			app.window.Update(func() {
				if err != nil {
					app.modelProblem = err.Error()
					return
				}
				app.model = settings
			})
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *desktop) observe() {
	var snapshot localrun.Snapshot
	connected := false
	for a.ctx.Err() == nil {
		var next localrun.Snapshot
		var err error
		if !connected {
			a.window.Update(func() { a.connecting = true })
			ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
			next, err = a.client.Connect(ctx)
			cancel()
		} else if snapshot.Active() {
			ctx, cancel := context.WithTimeout(a.ctx, 7*time.Second)
			next, err = a.client.Watch(ctx, snapshot.ID, snapshot.Progress.Sequence)
			cancel()
			if err == nil && next.ID == "" {
				continue
			}
		} else {
			if !delay(a.ctx, time.Second) {
				return
			}
			ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
			next, err = a.client.Get(ctx, "", "")
			cancel()
		}
		if err != nil {
			if a.ctx.Err() != nil {
				return
			}
			connected = false
			a.window.Update(func() {
				if a.connected {
					a.connectionEpoch++
				}
				a.connected = false
				a.connecting = false
				a.connectionError = err.Error()
			})
			if !a.waitToReconnect() {
				return
			}
			continue
		}
		previousID := snapshot.ID
		snapshot = next
		a.window.Update(func() {
			reconnected := !a.connected
			a.connected, a.connecting, a.connectionError = true, false, ""
			// A command can publish a new execution while a poll of the previous one is in flight.
			if next.ID != previousID || a.snapshot.ID == "" || a.snapshot.ID == previousID {
				a.applySnapshot(next)
			}
			if reconnected {
				a.refreshDevices()
				a.checkRequest()
			}
		})
		connected = true
	}
}

func (a *desktop) waitToReconnect() bool {
	timer := time.NewTimer(700 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-a.ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-a.retry:
		return true
	}
}

func delay(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *desktop) applySnapshot(value localrun.Snapshot) {
	if value.ID == "" || (value.ID == a.snapshot.ID && value.Progress.Sequence < a.snapshot.Progress.Sequence) {
		return
	}
	a.snapshot = value
	if a.pending != nil && value.Request.RequestID == a.pending.request.RequestID {
		a.pending, a.runProblem = nil, ""
	}
	if value.Request.RequestID == a.autoOpen && !value.Active() {
		a.autoOpen = ""
		if !value.Suite {
			path := value.Progress.ReportPath
			if path == "" && len(value.Progress.Cases) == 1 {
				path = value.Progress.Cases[0].ReportPath
			}
			if path != "" {
				a.openReport(path)
			}
		}
	}
}

func (a *desktop) refreshDevices() {
	if a.refreshing || !a.connected {
		return
	}
	a.refreshing = true
	epoch := a.connectionEpoch
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 4*time.Second)
		defer cancel()
		devices, err := a.client.Devices(ctx)
		a.window.Update(func() {
			a.refreshing = false
			if !a.connected || epoch != a.connectionEpoch {
				a.refreshDevices()
				return
			}
			a.deviceError = ""
			if err != nil {
				a.deviceError = "无法读取设备列表：" + err.Error()
				return
			}
			a.devices = devices
		})
	}()
}

func (a *desktop) selectedOnline() bool {
	return a.deviceError == "" && slices.ContainsFunc(a.devices, func(device gnosis.DeviceInfo) bool {
		return device.ID == a.deviceID && device.State == "device"
	})
}

func (a *desktop) canStart() bool {
	return a.connected && !a.refreshing && !a.commandBusy && !a.snapshot.Active() &&
		a.doc != nil && (!a.doc.Suite() || a.doc.Rows() > 0) &&
		a.selectedOnline() && (a.pending == nil || a.pending.checked)
}

func (a *desktop) start() {
	if !a.canStart() {
		return
	}
	if pending := a.pending; pending != nil {
		path := a.doc.Path()
		if path == "" {
			path = "保存后使用所选路径"
		}
		message := fmt.Sprintf("上一次请求仍待确认\n%s · %s\n%s\n\n这次准备执行\n%s · %s\n%s\n\n执行进程会检查设备是否被占用。",
			pending.title, pending.request.DeviceID, pending.request.Path, documentTitle(a.doc), a.selectedDeviceLabel(), path)
		if !a.confirm("发起一次新的执行？", message, "确认发起新执行") || !a.canStart() {
			return
		}
	}
	if a.doc.Dirty() && (!a.confirm("保存后执行", "将保存当前编辑，再执行校验通过的文件。", "保存并执行") || !a.saveAsset(false)) {
		return
	}
	if !a.canStart() {
		return
	}
	if _, _, err := gnosis.Load(a.doc.Path()); err != nil {
		a.runProblem = err.Error()
		return
	}
	request := localrun.StartRequest{RequestID: rand.Text(), Path: a.doc.Path(), DeviceID: a.deviceID}
	title := documentTitle(a.doc)
	priorAutoOpen := a.autoOpen
	a.commandBusy, a.autoOpen = true, request.RequestID
	a.runProblem = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		snapshot, err := a.client.Start(ctx, request)
		cancel()
		var rejected *localrun.Rejected
		unknown := err != nil && !errors.As(err, &rejected)
		if unknown && a.ctx.Err() == nil {
			ctx, cancel = context.WithTimeout(a.ctx, 6*time.Second)
			if _, connectErr := a.client.Connect(ctx); connectErr == nil {
				if known, queryErr := a.client.Get(ctx, "", request.RequestID); queryErr == nil {
					snapshot, err, unknown = known, nil, false
				}
			}
			cancel()
		}
		a.window.Update(func() {
			a.commandBusy = false
			switch {
			case unknown:
				if a.snapshot.Request.RequestID != request.RequestID {
					a.pending = &pendingStart{request: request, title: title}
				}
			case err != nil:
				if a.autoOpen == request.RequestID {
					a.autoOpen = priorAutoOpen
				}
				a.runProblem = err.Error()
				a.refreshDevices()
			default:
				a.pending = nil
				a.applySnapshot(snapshot)
			}
		})
	}()
}

func (a *desktop) checkRequest() {
	pending := a.pending
	if pending == nil || a.commandBusy || !a.connected {
		return
	}
	a.commandBusy, pending.checking = true, true
	pending.checkNote = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 4*time.Second)
		defer cancel()
		snapshot, err := a.client.Get(ctx, "", pending.request.RequestID)
		a.window.Update(func() {
			a.commandBusy = false
			if a.pending != pending {
				return
			}
			pending.checking = false
			switch {
			case errors.Is(err, localrun.ErrUnknownExecution):
				pending.checked = true
			case err != nil:
				pending.checkNote = "暂时无法核对，请在连接恢复后重试。"
			case snapshot.Request.RequestID == pending.request.RequestID:
				a.applySnapshot(snapshot)
			}
		})
	}()
}

func (a *desktop) control(command string) {
	if !a.connected || a.commandBusy || !a.snapshot.Active() {
		return
	}
	id := a.snapshot.ID
	a.commandBusy = true
	a.runProblem = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		snapshot, err := a.client.Control(ctx, id, command)
		a.window.Update(func() {
			a.commandBusy = false
			if err != nil {
				a.runProblem = err.Error()
				return
			}
			a.applySnapshot(snapshot)
		})
	}()
}

func (a *desktop) openReport(path string) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".html" {
		a.runProblem = fmt.Sprintf("无法识别报告路径 %q", path)
		return
	}
	if err := mygo.Shell.OpenPath(path); err != nil {
		a.runProblem = err.Error()
	}
}

func (a *desktop) confirm(title, message, button string) bool {
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: a.window, Type: mygo.MessageQuestion, Title: title, Message: message,
		Buttons: []string{"取消", button}, DefaultButton: 0, CancelButton: 0,
	})
	return err == nil && result.Button == 1
}

func (a *desktop) showError(err error) { a.problem, a.message = err.Error(), "" }
