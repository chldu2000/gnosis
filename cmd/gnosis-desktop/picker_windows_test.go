package main

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gnosis"
	"gnosis/internal/assetform"
)

func TestPickRelativeLocatorFromScreenshot(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		flat, duplicate, offline bool
		want                     string
	}{
		{"settings-row", false, false, false, "within"}, {"offline-flat", true, false, true, "right_of"}, {"duplicate-label", false, true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &desktop{width: 1040}
			doc := app.addDocument(assetform.New(false))
			var pixels bytes.Buffer
			size := image.Pt(200, 120)
			if err := png.Encode(&pixels, image.NewRGBA(image.Rectangle{Max: size})); err != nil {
				t.Fatal(err)
			}
			tree := []gnosis.Element{{Type: "Screen", Bounds: image.Rectangle{Max: size}}}
			for row, label := range []string{"消息通知", "声音"} {
				y := row * 60
				depth := 1
				if !tc.flat {
					tree = append(tree, gnosis.Element{Type: "Row", Depth: 1, Bounds: image.Rect(0, y, 200, y+60)})
					depth++
				}
				if tc.duplicate {
					label = "消息通知"
				}
				var id string
				if !tc.duplicate {
					id = []string{"first-row-label", "second-row-label"}[row]
				}
				tree = append(tree, gnosis.Element{ID: id, Text: label, Depth: depth, Bounds: image.Rect(5, y+5, 85, y+50)},
					gnosis.Element{Type: "Switch", Depth: depth, Bounds: image.Rect(150, y+5, 190, y+50)})
			}
			app.picker = elementPicker{open: true, doc: doc, offline: tc.offline}
			app.picker.display(&gnosis.InterfaceCapture{Size: size, Screenshot: pixels.Bytes(), Elements: tree})
			view := ui.NewTester(app.view, 1040, 740)
			tapPicture := func(x, y float32) {
				screen := locate(t, view, "设备界面截图")
				view.ClickAt(screen.X+x/200*screen.W, screen.Y+y/120*screen.H)
			}
			tapPicture(165, 25)
			click(t, view, "用参照元素定位")
			if view.HasText("新增点击") {
				t.Fatal("could write an incomplete rule")
			}
			tapPicture(45, 25)
			if !view.HasText("写入目标 · Switch") {
				t.Fatal("reference click replaced the target")
			}
			if tc.duplicate {
				if view.HasText("新增点击") || !view.HasText("参照匹配 2 个元素，请选择能唯一识别参照的条件。") {
					t.Fatalf("ambiguous reference: %v", view.Texts())
				}
				click(t, view, "更换参照")
				click(t, view, "取消相对定位")
				if !view.HasText("新增点击") || doc.Rows() != 0 {
					t.Fatal("cancel did not restore direct selection")
				}
				return
			}
			click(t, view, "新增点击")
			path := filepath.Join(t.TempDir(), "settings.yaml")
			if err := doc.Save(path); err != nil {
				t.Fatal(err)
			}
			c, _, err := gnosis.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			l := c.Steps[0].Locator
			if l.Type == nil || *l.Type != "Switch" || l.Relative == nil || l.Relative.Relation != tc.want || l.Relative.Anchor.Text == nil || *l.Relative.Anchor.Text != "消息通知" {
				t.Fatalf("unexpected locator: %+v", l)
			}
		})
	}
}

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
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	source.Turns[0].AfterImage = "after.png"
	if err := os.WriteFile(filepath.Join(filepath.Dir(open.report), "after.png"), pixels.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(open.report, []byte("original report"), 0600); err != nil {
		t.Fatal(err)
	}
	uiTest := ui.NewTester(app.view, 1040, 740)
	click(t, uiTest, "复核坐标与截图")
	data, err := os.ReadFile(open.report)
	if !strings.Contains(app.picker.problem, "动作前截图") || !app.picker.open || err != nil || string(data) != "original report" || open.StepKind(0) != "tap_at" {
		t.Fatalf("missing image: %q, %q, %v", app.picker.problem, data, err)
	}
	click(t, uiTest, "点击后")
	locate(t, uiTest, "动作后截图")
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
	if choice := app.picker.choices[app.picker.candidate]; choice.Count != 1 || choice.Locator.Text == nil || *choice.Locator.Text != "Messages" {
		t.Fatal("screenshot selection defaulted to the duplicate ID")
	}
	click(t, view, "搜索元素")
	view.Type("Mail")
	if !view.HasText("写入目标 · Messages") || view.HasText("新增点击") || view.HasText("填入当前步骤") || view.HasText("新增断言") {
		t.Fatal("filtered-out target can still be written")
	}
	click(t, view, "  Mail")
	if !view.HasText("写入目标 · Mail") || app.picker.choices[app.picker.candidate].Count != 1 {
		t.Fatal("tree selection did not choose a unique locator for Mail")
	}
	if !view.HasText("ID=switch · 2 个匹配") {
		t.Fatalf("missing duplicate indicator: %v", view.Texts())
	}
	click(t, view, "ID=switch · 2 个匹配")
	if !view.HasText("匹配 2 个元素，点击步骤需要唯一定位器或索引。") {
		t.Fatal("ambiguous candidate has no warning")
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

func TestReviewSwitchesScreensAtSameScaleAndKeepsOriginalTap(t *testing.T) {
	app := &desktop{width: 1040}
	dir := t.TempDir()
	before, after := image.Pt(100, 200), image.Pt(200, 100)
	for file, size := range map[string]image.Point{"before.png": before, "after.png": after} {
		var out bytes.Buffer
		if err := png.Encode(&out, image.NewRGBA(image.Rectangle{Max: size})); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	draft := &gnosis.CaseDraft{YAML: "steps: [{tap_at: {x: 0.25, y: 0.6}}]", Expected: []string{"Amber detail"},
		Turns: []gnosis.DraftTurn{{Row: 0, Coordinate: true, Before: &gnosis.InterfaceCapture{Size: before, Elements: []gnosis.Element{
			{Text: "Featured", Bounds: image.Rect(0, 0, 100, 50)}, {Text: "Amber", Bounds: image.Rect(0, 100, 100, 150)},
		}}, BeforeImage: "before.png", AfterImage: "after.png"}},
	}
	form, err := assetform.FromDraft(draft)
	if err != nil {
		t.Fatal(err)
	}
	doc := app.addDocument(form)
	doc.review, doc.report = draft, filepath.Join(dir, "report.html")
	view := ui.NewTester(app.view, 1040, 740)
	click(t, view, "复核坐标与截图")
	click(t, view, "最大化")
	screen := locate(t, view, "设备界面截图")
	x, y := screen.X+screen.W*.25, screen.Y+screen.H*.6
	pixel := view.Image().RGBAAt(int(x), int(y))
	if pixel.R < 150 || pixel.G > 90 || pixel.B > 140 {
		t.Fatalf("expected original tap marker, got %v", pixel)
	}
	view.ClickAt(x, y)
	click(t, view, "点击后")
	result := locate(t, view, "动作后截图")
	if math.Abs(float64(result.W-screen.H)) > 0.01 || math.Abs(float64(result.H-screen.W)) > 0.01 || !view.HasText("Amber detail") || view.HasText("填入当前步骤") {
		t.Fatalf("review switched scale or exposed write actions: %v -> %v", screen, result)
	}
	click(t, view, "点击前")
	if !view.HasText("写入目标 · Amber") || screen != locate(t, view, "设备界面截图") {
		t.Fatal("selection or scale lost switching back")
	}
	click(t, view, "用参照元素定位")
	click(t, view, "Featured")
	if app.picker.relativeLocator().Relative.Relation != "below" {
		t.Fatal("offline reference was not resolved from the saved capture")
	}
	click(t, view, "填入当前步骤")
	if doc.StepKind(0) != "tap" {
		t.Fatal("coordinate was not replaced")
	}
	click(t, view, "复核坐标与截图")
	if app.picker.tap == nil || app.picker.tap.X != .25 || app.picker.tap.Y != .6 {
		t.Fatal("draft edit changed the original click evidence")
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

func TestPickerResizeKeepsSelectionAndFitsWorkbench(t *testing.T) {
	app := &desktop{width: 1200}
	doc := app.addDocument(assetform.New(false))
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 100, 200))); err != nil {
		t.Fatal(err)
	}
	capture := &gnosis.InterfaceCapture{Size: image.Pt(100, 200), Screenshot: pixels.Bytes(), Elements: []gnosis.Element{
		{Text: "Top", Bounds: image.Rect(0, 0, 100, 100)},
		{Text: "Bottom", Bounds: image.Rect(0, 100, 100, 200)},
	}}
	app.picker = elementPicker{open: true, doc: doc}
	app.picker.display(capture)
	view := ui.NewTester(app.view, 1200, 860)
	start := locate(t, view, "元素选择器")
	screen := locate(t, view, "设备界面截图")
	grip := locate(t, view, "调整选择器大小")
	x, y := grip.X+grip.W/2, grip.Y+grip.H/2
	view.Press(x, y)
	view.Move(x+50, y+40)
	view.Release(x+50, y+40)
	resized := locate(t, view, "元素选择器")
	bigger := locate(t, view, "设备界面截图")
	if resized.W <= start.W || resized.H <= start.H || bigger.H <= screen.H {
		t.Fatalf("drag did not enlarge dialog and image: %v -> %v, %v -> %v", start, resized, screen, bigger)
	}
	view.ClickAt(bigger.X+bigger.W/2, bigger.Y+bigger.H*0.75)
	click(t, view, "最大化")
	full := locate(t, view, "元素选择器")
	if full.W <= resized.W || full.H <= resized.H || app.picker.selected != 1 {
		t.Fatalf("maximize lost size or selection: %v, %d", full, app.picker.selected)
	}
	click(t, view, "还原")
	if got := locate(t, view, "元素选择器"); got != resized {
		t.Fatalf("restore: %v, want %v", got, resized)
	}
	click(t, view, "关闭")
	app.picker = elementPicker{open: true, offline: true, doc: doc}
	app.picker.search = "missing"
	app.picker.display(capture)
	view.Frame()
	if !view.HasText("没有匹配的元素") {
		t.Fatal("replacing the capture should keep the filter and explain the empty result")
	}
	if got := locate(t, view, "元素选择器"); got != resized {
		t.Fatalf("offline picker reopened at %v, want %v", got, resized)
	}
	view.SetSize(760, 540)
	shrunk := locate(t, view, "元素选择器")
	if shrunk.X < 0 || shrunk.Y < 0 || shrunk.X+shrunk.W > 760 || shrunk.Y+shrunk.H > 540 {
		t.Fatalf("picker outside workbench: %v", shrunk)
	}
	now := locate(t, view, "设备界面截图")
	view.ClickAt(now.X+now.W/2, now.Y+now.H/4)
	click(t, view, "新增点击")
	if app.picker.open || doc.Rows() != 1 || doc.StepKind(0) != "tap" {
		t.Fatal("could not apply locator in the smaller workbench")
	}
}
