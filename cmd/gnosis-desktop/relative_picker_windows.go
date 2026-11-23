package main

import (
	"fmt"
	"image"
	"slices"

	"github.com/egoist/mygo/ui"
	"gnosis"
)

type relativeSelection struct {
	anchor        int // -1 while the next screenshot or tree click chooses the reference
	anchorChoices []gnosis.LocatorCandidate
	anchorChoice  int
	rules         []gnosis.RelativeLocator
	rule          int
	preview       gnosis.RelativeMatches
	message       string
}

func (p *elementPicker) beginReference() {
	p.relative = &relativeSelection{anchor: -1}
	p.search = ""
}

func (p *elementPicker) pickReference(index int) {
	r := p.relative
	if index < 0 || index == p.selected {
		r.message = "请选择目标以外的参照元素"
		return
	}
	choices := p.capture.Candidates(index)
	if len(choices) == 0 {
		r.message = "这个参照元素没有可保存的属性，请另选参照"
		return
	}
	r.anchor, r.anchorChoices = index, choices
	r.anchorChoice = preferredReference(choices)
	p.search = ""
	p.buildRelativeRules()
}

// A unique label follows its row when a list reuses position-based IDs.
func preferredReference(choices []gnosis.LocatorCandidate) int {
	firstUnique := -1
	for i, c := range choices {
		if c.Count != 1 {
			continue
		}
		if firstUnique < 0 {
			firstUnique = i
		}
		if c.Locator.Text != nil || c.Locator.Description != nil {
			return i
		}
	}
	return max(0, firstUnique)
}

func (p *elementPicker) relativeLocator() gnosis.Locator {
	var loc gnosis.Locator
	if len(p.choices) > 0 {
		loc = p.choices[p.candidate].Locator
	}
	r := p.relative
	loc.Relative = &r.rules[r.rule]
	return loc
}

func (p *elementPicker) buildRelativeRules() {
	r := p.relative
	r.rules = p.capture.RelationsBetween(p.selected, r.anchor, r.anchorChoices[r.anchorChoice].Locator)
	r.rule = 0
	r.preview = gnosis.RelativeMatches{Container: -1}
	for i := range r.rules {
		r.rule = i
		r.preview = p.capture.PreviewRelative(p.relativeLocator())
		if len(r.preview.Targets) == 1 && r.preview.Targets[0] == p.selected {
			return
		}
	}
	if len(r.rules) > 0 {
		r.rule = 0
		r.preview = p.capture.PreviewRelative(p.relativeLocator())
	}
}

func relationLabel(r gnosis.RelativeLocator) string {
	switch r.Relation {
	case "within":
		if r.Levels == 0 {
			return "参照元素内部"
		}
		return fmt.Sprintf("共同容器（从参照向上 %d 层）", r.Levels)
	case "right_of":
		return "参照右侧，同一行"
	case "left_of":
		return "参照左侧，同一行"
	case "above":
		return "参照上方，同一列"
	case "below":
		return "参照下方，同一列"
	}
	return r.Relation
}

func chooseCondition(c *ui.Context, label string, choices []gnosis.LocatorCandidate, current int) int {
	labels := make([]string, len(choices))
	for i, ch := range choices {
		labels[i] = fmt.Sprintf("%s · %d 个匹配", locatorLabel(ch.Locator), ch.Count)
	}
	selected := labels[current]
	ui.Select(c.Key(label), &selected, labels).Label(label).FillWidth()
	if i := slices.Index(labels, selected); i >= 0 {
		return i
	}
	return current
}

func (a *desktop) relativeView(c *ui.Context, size ui.Point) {
	p := &a.picker
	r := p.relative
	ui.Row(c).Gap(4).Children(func() {
		if r.anchor >= 0 && ui.Button(c, "更换参照").Clicked() {
			p.beginReference()
		}
		if ui.Button(c, "取消相对定位").Clicked() {
			p.relative = nil
		}
	})
	if p.relative != r {
		return
	}
	if r.anchor < 0 {
		ui.Text(c, "在截图或控件树中选参照元素。橙框标出写入目标。").Wrap()
		if r.message != "" {
			ui.Text(c, r.message).Wrap()
		}
		return
	}
	ui.Scroll(c.Key("relative-options")).MaxHeight(size.Y * 0.40).FillWidth().Children(func() {
		ui.Column(c).Gap(4).FillWidth().Children(func() {
			ui.Text(c, "参照（蓝框） · "+elementLabel(p.capture.Elements[r.anchor])).Wrap()
			if i := chooseCondition(c, "参照条件", r.anchorChoices, r.anchorChoice); i != r.anchorChoice {
				r.anchorChoice = i
				p.buildRelativeRules()
			}
			if len(p.choices) > 0 {
				ui.Text(c, "目标条件（橙框）")
				if i := chooseCondition(c, "目标条件", p.choices, p.candidate); i != p.candidate {
					p.candidate = i
					p.buildRelativeRules()
				}
			}
			if len(r.rules) == 0 {
				ui.Text(c, "两个节点没有共同容器，也不在同一行或列，请更换参照。").Wrap()
				return
			}
			for i, rule := range r.rules {
				label := relationLabel(rule)
				var button ui.Element
				if i == r.rule {
					button = ui.PrimaryButton(c.Key(i), label)
				} else {
					button = ui.Button(c.Key(i), label)
				}
				if button.FillWidth().Clicked() {
					r.rule = i
					r.preview = p.capture.PreviewRelative(p.relativeLocator())
				}
			}
		})
	})
	if len(r.rules) == 0 {
		return
	}
	if len(r.preview.Anchors) != 1 {
		ui.Text(c, fmt.Sprintf("参照匹配 %d 个元素，请选择能唯一识别参照的条件。", len(r.preview.Anchors))).Wrap()
		return
	}
	info := fmt.Sprintf("参照 %d 个 · 目标 %d 个", len(r.preview.Anchors), len(r.preview.Targets))
	if scope := r.preview.Container; scope >= 0 {
		info += " · 容器 " + elementLabel(p.capture.Elements[scope])
	}
	ui.Text(c, info).Wrap()
	if len(r.preview.Targets) != 1 || r.preview.Targets[0] != p.selected {
		ui.Text(c, "请调整参照条件或关系，使规则唯一命中所选目标。").Wrap()
		return
	}
	a.pickerActions(c, p.relativeLocator())
}

func (p *elementPicker) drawRelative(painter *ui.Painter, frame ui.Rect, scale float32) {
	r := p.relative
	if r.anchor < 0 {
		return
	}
	stroke := func(index int, color ui.Color, width float32) {
		b := p.capture.Elements[index].Bounds.Intersect(image.Rectangle{Max: p.capture.Size})
		if !b.Empty() {
			painter.Stroke(ui.Rect{X: frame.X + float32(b.Min.X)*scale, Y: frame.Y + float32(b.Min.Y)*scale, W: float32(b.Dx()) * scale, H: float32(b.Dy()) * scale}, color, 0, width)
		}
	}
	if scope := r.preview.Container; scope >= 0 {
		stroke(scope, ui.Hex("#8053ad"), 2)
	}
	for _, i := range r.preview.Targets {
		if i != p.selected {
			stroke(i, ui.Hex("#e38824"), 2)
		}
	}
	for _, i := range r.preview.Anchors {
		stroke(i, ui.Hex("#1674bd"), 2)
	}
	stroke(r.anchor, ui.Hex("#1674bd"), 3)
}
