package gnosis_test

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gnosis"
)

func settingsRows(names ...string) []gnosis.Element {
	tree := []gnosis.Element{{Type: "Settings", Bounds: image.Rect(0, 0, 200, 200)}}
	for row, name := range names {
		top := row * 60
		tree = append(tree, gnosis.Element{Depth: 1, Type: "Row", Bounds: image.Rect(0, top, 200, top+60)},
			gnosis.Element{Depth: 2, Type: "LabelGroup"},
			gnosis.Element{Depth: 3, Text: name, Bounds: image.Rect(5, top+8, 85, top+40)},
			gnosis.Element{Depth: 2, Type: "Switch", Bounds: image.Rect(150, top+8, 190, top+40)})
	}
	return tree
}

func TestRelativeLocatorRunsAfterReorderingAndRefusesAmbiguity(t *testing.T) {
	for _, rule := range []string{"relation: within, levels: 2", "relation: right_of"} {
		path := filepath.Join(t.TempDir(), "settings.yaml")
		loc := "{type: Switch, relative: {anchor: {text: 消息通知}, " + rule + "}}"
		if err := os.WriteFile(path, []byte("steps:\n- tap: {locator: "+loc+"}\n- expect: {exists: "+loc+"}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		c, _, err := gnosis.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			names []string
			want  string
		}{
			{[]string{"消息通知", "声音"}, "tap (170,24)"}, {[]string{"声音", "消息通知"}, "tap (170,84)"}, {[]string{"消息通知", "消息通知"}, ""}, {[]string{"声音"}, ""},
		} {
			dev := &testDevice{trees: [][]gnosis.Element{settingsRows(tc.names...)}}
			record := runCase(t, dev, &testClock{}, c.Steps...)
			if tc.want != "" {
				if record.Result != gnosis.ResultPass || !slices.Contains(dev.calls, tc.want) {
					t.Fatalf("%s %v: %v %v", rule, tc.names, record.Result, dev.calls)
				}
			} else if record.Result != gnosis.ResultError || strings.Contains(strings.Join(dev.calls, ";"), "tap ") || dev.recognitions != 0 {
				t.Fatalf("unresolved reference issued input or OCR: %v %v", record.Result, dev.calls)
			}
		}
		tree := settingsRows("消息通知")
		tree = append(tree, tree[len(tree)-1])
		dev := &testDevice{trees: [][]gnosis.Element{tree}}
		record := runCase(t, dev, &testClock{}, c.Steps...)
		if record.Result != gnosis.ResultError || strings.Contains(strings.Join(dev.calls, ";"), "tap ") {
			t.Fatalf("ambiguous targets: %v", dev.calls)
		}
	}
}

func TestRelativeAbsenceWaitsForItsScope(t *testing.T) {
	r := gnosis.Locator{Text: locatorString("missing"), Relative: &gnosis.RelativeLocator{Relation: "within", Anchor: gnosis.Locator{Text: locatorString("panel")}}}
	step := gnosis.Step{Expect: &gnosis.Assertion{Absent: &r}}
	for _, tc := range []struct {
		name string
		want gnosis.Result
	}{{"other", gnosis.ResultFail}, {"panel", gnosis.ResultPass}} {
		dev := &testDevice{trees: [][]gnosis.Element{{{Text: tc.name}}}}
		if got := runCase(t, dev, &testClock{}, step); got.Result != tc.want || dev.recognitions != 0 {
			t.Fatalf("anchor %s: %v %v", tc.name, got.Result, dev.calls)
		}
	}
}

func TestSpatialReferencePreview(t *testing.T) {
	cap := gnosis.InterfaceCapture{Elements: []gnosis.Element{
		{Text: "A", Bounds: image.Rect(20, 20, 30, 30)},
		{Bounds: image.Rect(30, 20, 40, 30)}, {Bounds: image.Rect(10, 20, 20, 30)},
		{Bounds: image.Rect(21, 10, 31, 20)}, {Bounds: image.Rect(20, 30, 30, 40)},
		{Bounds: image.Rect(30, 30, 40, 40)}, {},
	}}
	anchor := gnosis.Locator{Text: locatorString("A")}
	for i, direction := range []string{"right_of", "left_of", "above", "below"} {
		rules := cap.RelationsBetween(i+1, 0, anchor)
		if len(rules) != 1 || rules[0].Relation != direction {
			t.Fatalf("%d: %+v", i, rules)
		}
		got := cap.PreviewRelative(gnosis.Locator{Relative: &rules[0]})
		if !slices.Equal(got.Targets, []int{i + 1}) || got.Container != -1 {
			t.Fatal(got)
		}
	}
}

func TestRelativeLocatorValidation(t *testing.T) {
	for _, relative := range []string{"{}", "{relation: nearby, anchor: {text: A}}", "{relation: within, anchor: {}}", "{relation: within, levels: -1, anchor: {text: A}}", "{relation: left_of, levels: 1, anchor: {text: A}}", "{relation: within, anchor: {text: A, index: 1}}", "{relation: within, anchor: {relative: {relation: within, anchor: {text: A}}}}", "{relation: within, anchor: {text: A}, extra: 2}"} {
		yaml := fmt.Sprintf("steps: [{tap: {locator: {relative: %s}}}]", relative)
		if err := gnosis.ValidateAsset("sample.yaml", []byte(yaml)); err == nil {
			t.Fatalf("accepted %s", relative)
		}
	}
}
