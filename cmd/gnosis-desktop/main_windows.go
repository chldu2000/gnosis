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
	"gnosis/internal/assetform"
	"gnosis/internal/localrun"
)

const windowTitle = "Gnosis"

type desktop struct {
	window *mygo.Window
	client *localrun.Client
	ctx    context.Context
	page   string
	doc    *assetform.Document
	row    int

	snapshot        localrun.Snapshot
	connected       bool
	connectionError string
	devices         []gnosis.DeviceInfo
	deviceID        string
	deviceError     string
	refreshing      bool
	commandBusy     bool
	pending         string
	autoOpen        string

	model   localrun.ModelSummary
	secret  string
	message string
	problem string
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
	app := &desktop{ctx: ctx, client: client, page: "edit", row: -1}
	mygo.App.WhenReady(func() {
		app.window = mygo.NewWindow(mygo.WindowOptions{
			Title: windowTitle, Width: 1040, Height: 740, MinWidth: 760, MinHeight: 540,
			Content: ui.View(app.view),
		})
		app.window.OnClose(func(event *mygo.CloseEvent) {
			if !app.canDiscard() {
				event.PreventDefault()
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
					app.showError(err)
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
				a.connected = false
				a.connectionError = err.Error()
			})
			if !delay(a.ctx, 700*time.Millisecond) {
				return
			}
			continue
		}
		snapshot = next
		a.window.Update(func() {
			if !a.connected {
				a.connected = true
				a.connectionError = ""
				if a.page == "run" {
					a.refreshDevices()
				}
			}
			a.applySnapshot(next)
		})
		connected = true
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
	if a.snapshot.ID == "" && a.doc == nil {
		a.page = "run"
		a.refreshDevices()
	}
	a.snapshot = value
	if a.pending != "" && value.Request.RequestID == a.pending {
		a.pending, a.problem = "", ""
	}
	if value.Request.RequestID == a.autoOpen && !value.Active() {
		a.autoOpen = ""
		if !value.Suite && len(value.Progress.Cases) == 1 && value.Progress.Cases[0].ReportPath != "" {
			a.openReport(value.Progress.Cases[0].ReportPath)
		}
	}
}

func (a *desktop) refreshDevices() {
	if a.refreshing || !a.connected {
		return
	}
	a.refreshing = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 4*time.Second)
		defer cancel()
		devices, err := a.client.Devices(ctx)
		a.window.Update(func() {
			a.refreshing = false
			a.devices = devices
			a.deviceError = ""
			if err != nil {
				a.deviceError = "无法读取设备列表：" + err.Error()
			}
		})
	}()
}

func (a *desktop) selectedOnline() bool {
	return slices.ContainsFunc(a.devices, func(device gnosis.DeviceInfo) bool {
		return device.ID == a.deviceID && device.State == "device"
	})
}

func (a *desktop) start() {
	if a.commandBusy || !a.connected || !a.selectedOnline() || a.doc == nil || a.snapshot.Active() {
		return
	}
	if a.pending != "" && !a.confirm("启动待确认", "上一次启动是否被接受仍无法确认。是否明确发起一次新的执行？", "新建执行") {
		return
	}
	if a.doc.Dirty() && (!a.confirm("保存后执行", "将保存当前编辑，再执行校验通过的文件。", "保存并执行") || !a.saveAsset(false)) {
		return
	}
	if _, _, err := gnosis.Load(a.doc.Path()); err != nil {
		a.showError(err)
		return
	}
	request := localrun.StartRequest{RequestID: rand.Text(), Path: a.doc.Path(), DeviceID: a.deviceID}
	a.commandBusy, a.autoOpen = true, request.RequestID
	a.problem, a.message = "", ""
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
				a.pending = request.RequestID
				a.problem = "启动待确认。可以核对请求，或明确发起新的执行。"
			case err != nil:
				a.autoOpen = ""
				a.showError(err)
				a.refreshDevices()
			default:
				a.pending = ""
				a.applySnapshot(snapshot)
			}
		})
	}()
}

func (a *desktop) checkRequest() {
	request := a.pending
	if request == "" || a.commandBusy {
		return
	}
	a.commandBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 4*time.Second)
		defer cancel()
		snapshot, err := a.client.Get(ctx, "", request)
		a.window.Update(func() {
			a.commandBusy = false
			if err != nil {
				a.problem = "启动待确认。仍无法核实上一次请求。"
				return
			}
			a.applySnapshot(snapshot)
		})
	}()
}

func (a *desktop) control(command string) {
	id := a.snapshot.ID
	a.commandBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
		defer cancel()
		snapshot, err := a.client.Control(ctx, id, command)
		a.window.Update(func() {
			a.commandBusy = false
			if err != nil {
				a.showError(err)
				return
			}
			a.applySnapshot(snapshot)
		})
	}()
}

func (a *desktop) openReport(path string) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".html" {
		a.showError(fmt.Errorf("无法识别报告路径 %q", path))
		return
	}
	if err := mygo.Shell.OpenPath(path); err != nil {
		a.showError(err)
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
