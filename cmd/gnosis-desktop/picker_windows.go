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
	choices                           []gnosis.LocatorCandidate
	review                            *gnosis.DraftTurn
	tap                               *gnosis.TapAt
	showAfter                         bool
	afterImage                        ui.ImageSource
	afterProblem                      string
	relative                          *relativeSelection
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
		label := "查看动作后截图与断言"
		if turn.Coordinate {
			label = "复核坐标与截图"
		}
		if ui.Button(c, label).Clicked() {
			a.openTurnReview(doc, turn)
		}
		return
	}
}

func (a *desktop) openTurnReview(doc *openedDocument, turn gnosis.DraftTurn) {
	p := elementPicker{open: true, offline: true, doc: doc, review: &turn, selected: -1, showAfter: !turn.Coordinate}
	if turn.Coordinate {
		p.tap = doc.review.RecordedTap(turn.Row)
		if turn.Before == nil {
			p.problem = "本轮缺少动作前界面材料"
		} else if data, err := sessionImage(doc.report, turn.BeforeImage); err != nil {
			p.problem = fmt.Sprintf("无法读取本轮动作前截图：%v", err)
		} else {
			capture := *turn.Before
			capture.Screenshot = data
			p.display(&capture)
		}
	}
	data, err := sessionImage(doc.report, turn.AfterImage)
	if err == nil {
		p.afterImage, err = ui.DecodeImage(data)
	}
	if err != nil {
		p.afterProblem = fmt.Sprintf("无法读取本轮动作后截图：%v", err)
	}
	a.picker = p
}

func (p *elementPicker) selectNode(index int) {
	if p.relative != nil && p.relative.anchor < 0 {
		p.pickReference(index)
		return
	}
	p.relative = nil
	p.selected, p.candidate = index, 0
	p.choices = p.capture.Candidates(index)
	for i, choice := range p.choices {
		if choice.Count == 1 {
			p.candidate = i
			break
		}
	}
}

func matchesSearch(e gnosis.Element, filter string) bool {
	return filter == "" || strings.Contains(strings.ToLower(strings.Join([]string{e.ID, e.Description, e.Text, e.Type}, " ")), filter)
}

func (p *elementPicker) display(capture *gnosis.InterfaceCapture) {
	p.relative = nil
	p.choices = nil
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
	p.problem = ""
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
	ui.DialogBase(c.Key("element-picker"), &p.open, func(back, panel ui.Element) {
		winW, winH := c.Size()
		maxW, maxH := max(1, winW-24), max(1, winH-24)
		if a.pickerSize.X == 0 {
			a.pickerSize = ui.Point{X: 820, Y: 550}
		}
		clampSize := func(w, h float32) ui.Point {
			return ui.Point{X: min(max(560, w), maxW), Y: min(max(380, h), maxH)}
		}
		size := clampSize(a.pickerSize.X, a.pickerSize.Y)
		if a.pickerExpanded {
			size = ui.Point{X: maxW, Y: maxH}
		}
		back.Background(ui.RGBA(0, 0, 0, 0.4))
		panel.Label("元素选择器").Width(size.X).Height(size.Y).Radius(8).Background(c.Theme().Background).Border(1, c.Theme().Border)
		ui.Column(c).Fill().Padding(12).Gap(6).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				title := "从设备选元素"
				if p.offline {
					title = "复核 AI 动作"
				}
				ui.Text(c, title).Bold().Grow(1)
				if !p.offline && ui.Button(c, "刷新界面").Disabled(p.busy).Clicked() {
					a.refreshCapture()
				}
				label := "最大化"
				if a.pickerExpanded {
					label = "还原"
				}
				if ui.Button(c.Key("expand"), label).Clicked() {
					a.pickerExpanded = !a.pickerExpanded
				}
				if ui.Button(c, "关闭").Clicked() {
					p.open = false
				}
			})
			if p.busy {
				ui.Text(c, "正在读取设备界面…")
				return
			}
			if p.review != nil && p.review.Coordinate {
				ui.Row(c.Key("phase")).Gap(6).Children(func() {
					if ui.Button(c, "点击前").Disabled(!p.showAfter).Clicked() {
						p.showAfter = false
					}
					if ui.Button(c, "点击后").Disabled(p.showAfter).Clicked() {
						p.showAfter = true
					}
					if !p.showAfter && p.tap != nil && p.bitmap != nil {
						ui.Text(c, fmt.Sprintf("红圈为原点击位置 · X %.1f%%  Y %.1f%%", p.tap.X*100, p.tap.Y*100)).FontSize(12)
					}
				})
			}
			if p.showAfter {
				p.afterView(c, size)
				return
			}
			if p.problem != "" {
				ui.Text(c, p.problem).Wrap().TextColor(c.Theme().Danger)
			}
			if p.capture == nil {
				return
			}
			ui.Row(c.Key("capture")).Grow(1).MinHeight(0).FillWidth().Gap(10).Children(func() {
				scale := p.imageScale(size)
				p.screenView(c, scale)
				ui.Column(c.Key("tree")).Grow(1).MinWidth(0).FillHeight().Gap(4).Children(func() {
					ui.TextInput(c, &p.search).Label("搜索元素").Placeholder("搜索 ID、文本、描述或类型").FillWidth()
					filter := strings.ToLower(strings.TrimSpace(p.search))
					ui.Scroll(c.Key("nodes")).FillWidth().Grow(1).MinHeight(0).Children(func() {
						ui.Column(c).FillWidth().Children(func() {
							found := false
							for i, e := range p.capture.Elements {
								label := elementLabel(e)
								if !matchesSearch(e, filter) {
									continue
								}
								text := strings.Repeat("  ", max(0, min(12, e.Depth))) + label
								found = true
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
									p.selectNode(i)
								}
							}
							if !found {
								ui.Text(c, "没有匹配的元素").TextColor(c.Theme().TextMuted)
							}
							p.scrollToNode = false
						})
					})
					if p.selected >= 0 {
						e := p.capture.Elements[p.selected]
						ui.Text(c, "写入目标 · "+elementLabel(e)).SingleLine().Tooltip(elementLabel(e))
						if p.relative != nil && p.relative.anchor < 0 {
							a.relativeView(c, size)
							return
						}
						if !matchesSearch(e, filter) {
							ui.Text(c, "当前筛选已隐藏该目标。请选择搜索结果或清除筛选后再写入。").Wrap()
							if ui.Button(c, "清除筛选").Clicked() {
								p.search, p.scrollToNode = "", true
							}
							return
						}
						choices := p.choices
						if p.relative != nil {
							a.relativeView(c, size)
							return
						}
						if ui.Button(c, "用参照元素定位").Clicked() {
							p.beginReference()
							return
						}
						ui.Scroll(c.Key("locator-options")).MaxHeight(size.Y * 0.46).FillWidth().Children(func() {
							ui.Column(c).Gap(4).FillWidth().Children(func() { a.locatorView(c, choices) })
						})
						if len(choices) > 0 {
							if count := choices[p.candidate].Count; count > 1 {
								ui.Text(c, fmt.Sprintf("匹配 %d 个元素，点击步骤需要唯一定位器或索引。", count)).Wrap()
							}
							a.pickerActions(c, choices[p.candidate].Locator)
						}
					}
				})
			})
		})
		if !a.pickerExpanded {
			grip := ui.Text(c.Key("resize"), "◢").Absolute().Right(0).Bottom(0).Width(24).Height(22).Label("调整选择器大小").Tooltip("拖动调整大小").TextColor(c.Theme().TextMuted).Cursor(ui.CursorResizeNWSE)
			if dx, dy, drag := grip.Dragged(); drag && (dx != 0 || dy != 0) {
				// The dialog stays centered, so each edge travels half the size change.
				a.pickerSize = clampSize(size.X+2*dx, size.Y+2*dy)
			}
		}
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

// Both phases use the same number of DIPs per device pixel, including after rotation.
func (p *elementPicker) imageScale(panel ui.Point) float32 {
	var w, h float32
	if p.capture != nil {
		w, h = float32(p.capture.Size.X), float32(p.capture.Size.Y)
	}
	if p.afterImage != nil {
		aw, ah := ui.ImageSize(p.afterImage)
		w, h = max(w, aw), max(h, ah)
	}
	height := panel.Y - 108
	if p.review != nil && p.review.Coordinate {
		height -= 42
	}
	return min(panel.X*0.46/max(1, w), max(1, height)/max(1, h))
}

func (p *elementPicker) afterView(c *ui.Context, size ui.Point) {
	ui.Row(c.Key("after")).FillWidth().Grow(1).MinHeight(0).Gap(10).Children(func() {
		if p.afterImage != nil {
			w, h := ui.ImageSize(p.afterImage)
			scale := p.imageScale(size)
			ui.Image(c, p.afterImage).Label("动作后截图").Width(w * scale).Height(h * scale).Shrink(0)
		}
		ui.Scroll(c).Grow(1).FillHeight().Children(func() {
			ui.Column(c).Gap(6).FillWidth().Children(func() {
				if p.afterProblem != "" {
					ui.Text(c, p.afterProblem).Wrap().TextColor(c.Theme().Danger)
				}
				ui.Text(c, p.review.Goal).Bold().Wrap()
				ui.Text(c, p.review.Note).Wrap()
				ui.Text(c, "原用例预期断言").Bold()
				for _, assertion := range p.doc.review.Expected {
					ui.Text(c, assertion).Wrap()
				}
			})
		})
	})
}

func (p *elementPicker) screenView(c *ui.Context, scale float32) {
	size := p.capture.Size
	w, h := float32(size.X)*scale, float32(size.Y)*scale
	screen := ui.Image(c.Key("screenshot"), p.bitmap).Label("设备界面截图").Width(w).Height(h).Shrink(0)
	screen.DrawOver(func(painter *ui.Painter, r ui.Rect) {
		if p.relative != nil {
			p.drawRelative(painter, r, scale)
		}
		if p.selected >= 0 {
			bounds := p.capture.Elements[p.selected].Bounds.Intersect(image.Rectangle{Max: size})
			if !bounds.Empty() {
				painter.Stroke(ui.Rect{X: r.X + float32(bounds.Min.X)*scale, Y: r.Y + float32(bounds.Min.Y)*scale, W: float32(bounds.Dx()) * scale, H: float32(bounds.Dy()) * scale}, ui.Hex("#f08426"), 0, 3)
			}
		}
		if p.tap != nil {
			x, y := r.X+r.W*float32(p.tap.X), r.Y+r.H*float32(p.tap.Y)
			ring := ui.Rect{X: x - 7, Y: y - 7, W: 14, H: 14}
			painter.Stroke(ring, ui.Hex("#ffffff"), 7, 5)
			painter.Stroke(ring, ui.Hex("#c02040"), 7, 2)
			painter.Fill(ui.Rect{X: x - 2, Y: y - 2, W: 4, H: 4}, ui.Hex("#c02040"), 2)
		}
	})
	x, y, over := screen.PointerPosition()
	if screen.Clicked() && over {
		p.selectNode(p.capture.Hit(image.Pt(int(x/scale), int(y/scale))))
		p.scrollToNode, p.search = true, ""
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

func (a *desktop) locatorView(c *ui.Context, cs []gnosis.LocatorCandidate) {
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
}

func (a *desktop) pickerActions(c *ui.Context, loc gnosis.Locator) {
	p := &a.picker
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
