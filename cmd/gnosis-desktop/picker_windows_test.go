package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gnosis"
	"gnosis/internal/assetform"
)

func TestOfflineReviewWithMissingBeforeImage(t *testing.T) {
	app := &desktop{width: 1040}
	source := &gnosis.CaseDraft{
		YAML:  "steps: [{tap_at: {x: 0.5, y: 0.4}}, {expect: {exists: {id: detail}}}]",
		Turns: []gnosis.DraftTurn{{Row: 0, Step: 1, Turn: 1, Goal: "打开详情", Coordinate: true, Before: &gnosis.InterfaceCapture{Size: image.Pt(100, 200)}, BeforeImage: "attachments/missing.png"}},
	}
	doc, err := assetform.FromDraft(source)
	if err != nil {
		t.Fatal(err)
	}
	open := app.addDocument(doc)
	open.review, open.report = source, filepath.Join(t.TempDir(), "report.html")
	if err := os.WriteFile(open.report, []byte("original report"), 0600); err != nil {
		t.Fatal(err)
	}
	uiTest := ui.NewTester(app.view, 1040, 740)
	click(t, uiTest, "复核坐标 · 查看动作前界面")
	data, err := os.ReadFile(open.report)
	if !strings.Contains(app.problem, "动作前截图") || app.picker.open || err != nil || string(data) != "original report" || open.StepKind(0) != "tap_at" {
		t.Fatalf("missing image: %q, %q, %v", app.problem, data, err)
	}
}

func TestPickerEditsWithoutDispatchAndKeepsExpectedValue(t *testing.T) {
	app := &desktop{width: 1040}
	doc := app.addDocument(assetform.New(false))
	doc.AddStep()
	doc.SetStepKind(0, "expect")
	doc.SetCheckKind(0, "attr_equals")
	for _, field := range assetform.Fields("expect", "attr_equals") {
		switch field.Label {
		case "属性":
			doc.SetField(0, field, "checked")
		case "期望值":
			doc.SetField(0, field, "true")
		}
	}
	doc.row = 0
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 100, 200))); err != nil {
		t.Fatal(err)
	}
	off := false
	capture := &gnosis.InterfaceCapture{Size: image.Pt(100, 200), Screenshot: data.Bytes(), Elements: []gnosis.Element{
		{Type: "Root", Bounds: image.Rect(0, 0, 100, 200)},
		{ID: "switch", Text: "Mail", Type: "Switch", Depth: 1, Checked: &off, Bounds: image.Rect(0, 0, 100, 100)},
		{ID: "switch", Text: "Messages", Type: "Switch", Depth: 1, Bounds: image.Rect(0, 100, 100, 200)},
	}}
	app.picker = elementPicker{open: true, doc: doc, selected: -1}
	app.picker.display(capture)
	view := ui.NewTester(app.view, 1040, 740)
	screen := locate(t, view, "设备界面截图")
	view.ClickAt(screen.X+screen.W/2, screen.Y+screen.H*0.75)
	if app.picker.selected != 2 {
		t.Fatalf("screenshot selected element %d, want Messages", app.picker.selected)
	}
	click(t, view, "  Mail")
	if !view.HasText("ID=switch · 2 个匹配") {
		t.Fatalf("missing duplicate indicator: %v", view.Texts())
	}
	click(t, view, "ID=switch · 文本=Mail · 1 个匹配")
	click(t, view, "填入当前步骤")
	dest := filepath.Join(t.TempDir(), "switch.yaml")
	if err := doc.Save(dest); err != nil {
		t.Fatal(err)
	}
	c, _, err := gnosis.Load(dest)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Steps[0].Expect.AttrEquals
	if !got.Value || got.Attribute != "checked" || *got.Locator.ID != "switch" || *got.Locator.Text != "Mail" || off {
		t.Fatalf("unexpected assertion %+v", got)
	}
}

func TestPickedLocatorSurvivesChangingNewAssertionKind(t *testing.T) {
	app := &desktop{width: 1040}
	doc := app.addDocument(assetform.New(false))
	app.picker.doc = doc
	id := "notification-switch"
	app.appendPicked("expect", gnosis.Locator{ID: &id})
	doc.SetCheckKind(0, "attr_equals")
	for _, f := range assetform.Fields("expect", "attr_equals") {
		switch f.Label {
		case "属性":
			doc.SetField(0, f, "checked")
		case "期望值":
			doc.SetField(0, f, "true")
		}
	}
	path := filepath.Join(t.TempDir(), "notification.yaml")
	if err := doc.Save(path); err != nil {
		t.Fatal(err)
	}
	c, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Steps[0].Expect.AttrEquals
	if got.Locator.ID == nil || *got.Locator.ID != id || !got.Value {
		t.Fatalf("unexpected assertion %+v", got)
	}
}
