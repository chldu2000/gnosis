package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gnosis"
	"gnosis/internal/assetform"
)

type elementPicker struct {
	open, offline, busy, scrollToNode bool
	doc                               *openedDocument
	serial, problem, search           string
	capture                           *gnosis.InterfaceCapture
	bitmap                            ui.ImageSource
	selected, candidate               int
}

func (a *desktop) openCaseDraft(report string) {
	draft, err := gnosis.ReadCaseDraft(report)
	if err != nil {
		a.showError(err)
		return
	}
	doc, err := assetform.FromDraft(draft)
	if err != nil {
		a.showError(err)
		return
	}
	opened := a.addDocument(doc)
	opened.review, opened.report = draft, report
}

func sessionImage(report, name string) ([]byte, error) {
	if name == "" {
		return nil, errors.New("本轮没有保存对应截图")
	}
	dir, err := os.OpenRoot(filepath.Dir(report))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadFile(filepath.FromSlash(name))
}

func (a *desktop) reviewStepView(c *ui.Context, doc *openedDocument) {
	if doc.review == nil {
		return
	}
	origin, ok := doc.DraftRow(doc.row)
	if !ok {
		return
	}
	for _, turn := range doc.review.Turns {
		if turn.Row != origin {
			continue
		}
		ui.Text(c, fmt.Sprintf("原目标 · %s\n第 %d 步，第 %d 轮 · %s", turn.Goal, turn.Step, turn.Turn, turn.Note)).Wrap()
		if ui.Button(c, "查看动作后截图与断言").Clicked() {
			data, err := sessionImage(doc.report, turn.AfterImage)
			if err != nil {
				a.showError(err)
				return
			}
			bitmap, err := ui.DecodeImage(data)
			if err != nil {
				a.showError(err)
				return
			}
			a.evidence, a.evidenceOpen = bitmap, true
		}
		ui.Modal(c.Key("turn-evidence"), &a.evidenceOpen, func() {
			ui.Column(c).Width(min(680, float32(a.width)-40)).Height(480).Gap(8).Padding(12).Children(func() {
				ui.Text(c, turn.Goal).Bold().Wrap()
				ui.Row(c).Grow(1).MinHeight(0).FillWidth().Gap(10).Children(func() {
					ui.Image(c, a.evidence).Width(260).FillHeight()
					ui.Scroll(c).Grow(1).FillHeight().Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							ui.Text(c, turn.Note).Wrap()
							ui.Text(c, "原用例预期断言").Bold()
							for _, assertion := range doc.review.Expected {
								ui.Text(c, assertion).Wrap()
							}
						})
					})
				})
				if ui.Button(c, "关闭").Clicked() {
					a.evidenceOpen = false
					a.evidence = nil
				}
			})
		})
		if turn.Coordinate && ui.Button(c, "复核坐标 · 查看动作前界面").Clicked() {
			if turn.Before == nil {
				a.showError(errors.New("本轮缺少动作前界面材料"))
				return
			}
			data, err := sessionImage(doc.report, turn.BeforeImage)
			if err != nil {
				a.showError(fmt.Errorf("无法读取本轮动作前截图：%w", err))
				return
			}
			capture := *turn.Before
			capture.Screenshot = data
			a.picker = elementPicker{open: true, offline: true, doc: doc, selected: -1}
			a.picker.display(&capture)
		}
		return
	}
}

func (p *elementPicker) display(capture *gnosis.InterfaceCapture) {
	p.capture, p.bitmap, p.selected, p.candidate = nil, nil, -1, 0
	if capture.Size.X <= 0 || capture.Size.Y <= 0 {
		p.problem = "无效的界面尺寸"
		return
	}
	bitmap, err := ui.DecodeImage(capture.Screenshot)
	if err != nil {
		p.problem = err.Error()
		return
	}
	p.capture, p.bitmap, p.selected, p.candidate = capture, bitmap, -1, 0
	p.problem, p.search = "", ""
}

func (a *desktop) openLivePicker(doc *openedDocument) {
	a.picker = elementPicker{open: true, doc: doc, serial: a.deviceID, selected: -1}
	a.refreshCapture()
}

func (a *desktop) refreshCapture() {
	p := &a.picker
	if p.serial == "" || !a.connected {
		p.problem = "请先在右栏连接并选定设备"
		p.capture, p.bitmap = nil, nil
		return
	}
	p.busy = true
	p.problem = ""
	ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
	serial, owner := p.serial, p.doc
	epoch := a.captureEpoch + 1
	a.captureEpoch = epoch
	go func() {
		defer cancel()
		capture, err := a.client.Capture(ctx, serial)
		a.window.Update(func() {
			if a.captureEpoch != epoch || !p.open || p.offline || p.doc != owner {
				return
			}
			p.busy = false
			if err != nil {
				p.problem = err.Error()
				p.capture = nil
				p.bitmap = nil
				return
			}
			p.display(capture)
		})
	}()
}

func (a *desktop) pickerView(c *ui.Context) {
	p := &a.picker
	ui.Modal(c.Key("element-picker"), &p.open, func() {
		ui.Column(c).Width(min(790, float32(a.width)-36)).Height(510).Padding(12).Gap(6).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				title := "从设备选元素"
				if p.offline {
					title = "动作前界面 · 复核定位器"
				}
				ui.Text(c, title).Bold().Grow(1)
				if !p.offline && ui.Button(c, "刷新界面").Disabled(p.busy).Clicked() {
					a.refreshCapture()
				}
				if ui.Button(c, "关闭").Clicked() {
					p.open = false
				}
			})
			if p.busy {
				ui.Text(c, "正在读取设备界面…")
				return
			}
			if p.problem != "" {
				ui.Text(c, p.problem).Wrap().TextColor(c.Theme().Danger)
			}
			if p.capture == nil {
				return
			}
			ui.Row(c.Key("capture")).Grow(1).MinHeight(0).FillWidth().Gap(10).Children(func() {
				p.screenView(c)
				ui.Column(c.Key("tree")).Grow(1).MinWidth(0).FillHeight().Gap(4).Children(func() {
					ui.TextInput(c, &p.search).Label("搜索元素").Placeholder("搜索 ID、文本、描述或类型").FillWidth()
					ui.Scroll(c.Key("nodes")).FillWidth().Grow(1).MinHeight(0).Children(func() {
						ui.Column(c).FillWidth().Children(func() {
							filter := strings.ToLower(strings.TrimSpace(p.search))
							for i, e := range p.capture.Elements {
								label := elementLabel(e)
								if filter != "" && !strings.Contains(strings.ToLower(strings.Join([]string{e.ID, e.Description, e.Text, e.Type}, " ")), filter) {
									continue
								}
								text := strings.Repeat("  ", max(0, min(12, e.Depth))) + label
								var item ui.Element
								if i == p.selected {
									item = ui.PrimaryButton(c.Key(i), text)
								} else {
									item = ui.Button(c.Key(i), text)
								}
								if i == p.selected && p.scrollToNode {
									item.ScrollIntoView()
								}
								if item.FillWidth().Tooltip(label).Clicked() {
									p.selected, p.candidate = i, 0
								}
							}
							p.scrollToNode = false
						})
					})
					if p.selected >= 0 {
						a.locatorView(c)
					}
				})
			})
		})
	})
}

func elementLabel(e gnosis.Element) string {
	for _, v := range []string{e.Text, e.Description, e.ID, e.Type} {
		if v != "" {
			return v
		}
	}
	return "无名称元素"
}

func (p *elementPicker) screenView(c *ui.Context) {
	size := p.capture.Size
	scale := min(float32(320)/float32(size.X), float32(432)/float32(size.Y))
	w, h := float32(size.X)*scale, float32(size.Y)*scale
	screen := ui.Image(c.Key("screenshot"), p.bitmap).Label("设备界面截图").Width(w).Height(h).Shrink(0)
	if p.selected >= 0 {
		bounds := p.capture.Elements[p.selected].Bounds.Intersect(image.Rectangle{Max: size})
		if !bounds.Empty() {
			screen.DrawOver(func(painter *ui.Painter, r ui.Rect) {
				painter.Stroke(ui.Rect{X: r.X + float32(bounds.Min.X)*scale, Y: r.Y + float32(bounds.Min.Y)*scale, W: float32(bounds.Dx()) * scale, H: float32(bounds.Dy()) * scale}, ui.Hex("#f08426"), 0, 3)
			})
		}
	}
	x, y, over := screen.PointerPosition()
	if screen.Clicked() && over {
		p.selected = p.capture.Hit(image.Pt(int(x/scale), int(y/scale)))
		p.candidate, p.scrollToNode, p.search = 0, true, ""
	}
}

func locatorLabel(l gnosis.Locator) string {
	var parts []string
	for _, f := range []struct {
		key   string
		value *string
	}{{"ID", l.ID}, {"文本", l.Text}, {"描述", l.Description}, {"类型", l.Type}} {
		if f.value != nil {
			parts = append(parts, f.key+"="+*f.value)
		}
	}
	return strings.Join(parts, " · ")
}

func (a *desktop) locatorView(c *ui.Context) {
	p := &a.picker
	e := p.capture.Elements[p.selected]
	ui.Text(c, fmt.Sprintf("%s · %v", e.Type, e.Bounds)).Wrap().FontSize(12)
	for _, attr := range []struct {
		key   string
		value *bool
	}{{"enabled", e.Enabled}, {"checked", e.Checked}} {
		if attr.value != nil {
			ui.Text(c, fmt.Sprintf("%s = %t", attr.key, *attr.value)).FontSize(12)
		}
	}
	cs := p.capture.Candidates(p.selected)
	if len(cs) == 0 {
		ui.Text(c, "该元素没有可用的定位条件")
		return
	}
	for i, choice := range cs {
		label := fmt.Sprintf("%s · %d 个匹配", locatorLabel(choice.Locator), choice.Count)
		var b ui.Element
		if i == p.candidate {
			b = ui.PrimaryButton(c.Key(i), label)
		} else {
			b = ui.Button(c.Key(i), label)
		}
		if b.FillWidth().Clicked() {
			p.candidate = i
		}
	}
	loc := cs[p.candidate].Locator
	doc := p.doc
	ui.Row(c.Key("apply")).Gap(4).Children(func() {
		if ui.Button(c, "填入当前步骤").Disabled(doc.row < 0).Clicked() {
			if doc.StepKind(doc.row) == "tap_at" {
				doc.SetStepKind(doc.row, "tap")
			}
			if err := doc.ApplyLocator(doc.row, loc); err != nil {
				p.problem = err.Error()
			} else {
				p.open = false
				doc.focusRow = true
			}
		}
		if ui.Button(c, "新增点击").Clicked() {
			a.appendPicked("tap", loc)
		}
		if ui.Button(c, "新增断言").Clicked() {
			a.appendPicked("expect", loc)
		}
	})
}

func (a *desktop) appendPicked(kind string, l gnosis.Locator) {
	p := &a.picker
	doc := p.doc
	doc.AddStep()
	doc.row = doc.Rows() - 1
	doc.SetStepKind(doc.row, kind)
	if kind == "expect" {
		doc.SetCheckKind(doc.row, "exists")
	}
	if err := doc.ApplyLocator(doc.row, l); err != nil {
		p.problem = err.Error()
		return
	}
	p.open = false
	doc.focusRow = true
}
