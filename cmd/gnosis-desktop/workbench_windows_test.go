package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"gnosis"
	"gnosis/internal/assetform"
	"gnosis/internal/localrun"
)

func openFixture(t *testing.T, a *desktop, path, contents string) *openedDocument {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	doc := a.loadAsset(path)
	if doc == nil || a.problem != "" {
		t.Fatalf("opening %s: %s", path, a.problem)
	}
	return doc
}

func locate(t *testing.T, view *ui.Tester, label string) ui.Rect {
	t.Helper()
	rect, ok := view.Find(label)
	if !ok {
		t.Fatalf("missing %q in %v", label, view.Texts())
	}
	return rect
}

func click(t *testing.T, view *ui.Tester, label string) {
	t.Helper()
	if err := view.Click(label); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchKeepsLaunchControlsAboveResults(t *testing.T) {
	for _, width := range []int{1040, 760} {
		a := &desktop{width: width, connected: true, deviceID: "fixture", devices: []gnosis.DeviceInfo{
			{Platform: "Android", Name: "Pixel 8", ID: "fixture", State: "device"},
		}}
		openFixture(t, a, filepath.Join(t.TempDir(), "case.yaml"), "name: 登录\nsteps: []\n")
		a.snapshot = localrun.Snapshot{ID: "done", Request: localrun.StartRequest{DeviceID: "fixture", Path: a.doc.Path()},
			Progress: gnosis.ExecutionProgress{State: gnosis.ExecutionCancelled, Cases: []gnosis.CaseProgress{
				{Name: "上次用例", State: gnosis.ExecutionCancelled, ReportPath: filepath.Join(t.TempDir(), "report.html")},
			}},
		}
		view := ui.NewTester(a.view, width, 540)
		prepare := locate(t, view, "准备执行")
		start := locate(t, view, "开始执行")
		recent := locate(t, view, "最近一次执行")
		if prepare.X <= locate(t, view, "登录").X || prepare.Y >= start.Y || start.Y >= recent.Y {
			t.Fatalf("unexpected panel order at %dpx: prepare=%+v start=%+v recent=%+v", width, prepare, start, recent)
		}
		locate(t, view, "打开部分报告")
		a.deviceName = "Pixel 8 · Android"
		a.devices[0].State = "offline"
		a.devices = append(a.devices, gnosis.DeviceInfo{Platform: "Android", Name: "Pixel 6", ID: "replacement", State: "device"})
		view.Frame()
		locate(t, view, "Pixel 8 · Android · fixture 已离线")
		if a.canStart() || a.deviceID != "fixture" {
			t.Fatal("device loss did not preserve and disable the selected device")
		}
		click(t, view, "选择在线设备")
		click(t, view, "Pixel 6 · Android · replacement")
		if !a.canStart() || a.deviceID != "replacement" {
			t.Fatal("replacement device could not be selected")
		}

		a.pending = &pendingStart{request: localrun.StartRequest{Path: a.doc.Path(), DeviceID: "fixture"}, title: "原用例"}
		view.Frame()
		locate(t, view, "启动待确认")
		if view.HasText("开始执行") || view.HasText("发起新执行") || a.canStart() {
			t.Fatal("unverified request allowed a start")
		}
		a.pending.checked = true
		view.Frame()
		if locate(t, view, "发起新执行…").Y >= locate(t, view, "启动待确认").Y {
			t.Fatal("new start is below the pending status")
		}
		a.pending.checking, a.commandBusy = true, true
		view.Frame()
		if locate(t, view, "发起新执行…").Y >= locate(t, view, "启动待确认").Y || a.canStart() {
			t.Fatal("rechecking moved or enabled the new start")
		}
		a.commandBusy = false
		a.pending.request.RequestID = "original"
		a.applySnapshot(localrun.Snapshot{ID: "original-run", Request: a.pending.request,
			Progress: gnosis.ExecutionProgress{State: gnosis.ExecutionPaused}})
		view.Frame()
		if a.pending != nil || a.snapshot.ID != "original-run" || a.canStart() {
			t.Fatal("recovering the original request left a new start available")
		}
		locate(t, view, "继续")

		a.snapshot.Progress.Cases = []gnosis.CaseProgress{{Name: "保留的用例", State: gnosis.ExecutionCancelled,
			ReportPath: filepath.Join(t.TempDir(), "report.html")}}
		a.connected = false
		a.connectionError = "pipe closed"
		a.snapshot.Progress.State = gnosis.ExecutionPaused
		view.Frame()
		locate(t, view, "上次同步")
		locate(t, view, "打开部分报告")
		if view.HasText("准备执行") || a.canStart() {
			t.Fatal("disconnected window offers to start an execution")
		}
	}
}

func TestWorkbenchKeepsDraftAndFocusWhileProgressChanges(t *testing.T) {
	dir := t.TempDir()
	a := &desktop{width: 1040, connected: true}
	original := "name: 同名用例\nsteps:\n  - name: 第一步\n    wait: {duration: 1s}\n  - name: 第二步\n    wait: {duration: 1s}\n"
	active := openFixture(t, a, filepath.Join(dir, "running.yaml"), original)
	other := openFixture(t, a, filepath.Join(dir, "other.yaml"), original)
	a.selectDocument(active)
	a.snapshot = localrun.Snapshot{
		ID: "running", Request: localrun.StartRequest{Path: active.Path(), DeviceID: "fixture"},
		CaseSources: []gnosis.CaseSource{{Path: active.Path(), Digest: sha256.Sum256([]byte(original)), Name: active.Name(), Steps: 2}},
		Progress:    gnosis.ExecutionProgress{State: gnosis.ExecutionRunning, Case: 1, Step: 2},
	}
	view := ui.NewTester(a.view, 1040, 740)
	click(t, view, "定位步骤")
	if active.row != 1 || a.liveStep() != 1 {
		t.Fatal("did not choose the step in the loaded version")
	}
	a.selectDocument(other)
	view.Frame()
	click(t, view, "2  第二步")
	view.Key(0, ui.KeyUp)
	if other.row != 0 {
		t.Fatal("up arrow did not select the preceding step")
	}
	view.Key(0, ui.KeyDown)
	if other.row != 1 {
		t.Fatal("down arrow did not restore the next step")
	}
	label := locate(t, view, assetform.StepName.Label)
	view.ClickAt(label.X+12, label.Y+label.H+20)
	if focused, _ := view.TextInputActive(); !focused {
		t.Fatalf("step name input below %+v did not receive focus; view: %v", label, view.Texts())
	}
	view.Type("草稿")
	a.snapshot.Progress.Step = 1
	view.Frame()
	if focused, _ := view.TextInputActive(); !focused {
		t.Fatal("progress update moved focus from the editor")
	}
	if !other.Dirty() {
		t.Fatal("editing the step did not mark the draft")
	}
	click(t, view, "打开当前用例")
	if a.doc != active || active.row != 0 || !other.Dirty() || other.row != 1 {
		t.Fatal("opening the running case lost the other document's draft or selected step")
	}
	active.SetName("已编辑")
	view.Frame()
	if a.liveStep() != -1 || view.HasText("本次执行位置") {
		t.Fatal("the current version was mismatched and still highlighted")
	}
	a.width = 760
	view.SetSize(760, 540)
	view.Frame()
	click(t, view, "用例与套件")
	click(t, view, "同名用例 · 未保存")
	if a.doc != other || other.row != 1 || !other.Dirty() {
		t.Fatal("compact file chooser lost the parked file")
	}
	view.Key(ui.Ctrl, ui.KeyS)
	if other.Dirty() {
		t.Fatal("Ctrl+S did not save the selected document")
	}
}
