package main

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"
	"gnosis/internal/assetform"
)

const filesBreakpoint = 980

func (a *desktop) view(c *ui.Context) {
	wide := a.width >= filesBreakpoint
	theme := c.Theme()
	canvas, panel := theme.Background, theme.Surface
	ui.Column(c).Fill().Background(canvas).Children(func() {
		ui.Row(c.Key("header")).FillWidth().Padding(9, 15).Gap(10).AlignItems(ui.Center).Background(panel).Children(func() {
			ui.Text(c, "Gnosis").FontSize(23).Bold().Grow(1)
			if !wide && ui.Button(c.Key("file-picker"), "用例与套件").Clicked() {
				a.filesOpen = true
			}
			ui.Text(c, a.connectionLabel()).FontSize(12).TextColor(theme.TextMuted)
			if ui.Button(c, "模型设置").Clicked() {
				a.modelOpen = true
			}
		})
		ui.Divider(c)
		ui.Row(c.Key("workbench")).Grow(1).MinHeight(0).FillWidth().AlignItems(ui.Stretch).Gap(12).Padding(10, 12).Children(func() {
			if wide {
				ui.Column(c.Key("files")).Width(174).MinHeight(0).Padding(9).Gap(9).
					Background(panel).Border(1, theme.Border).Radius(5).Children(func() {
					a.filesView(c)
				})
			}
			ui.Column(c.Key("editor")).Grow(1).MinWidth(0).MinHeight(0).Gap(8).Children(func() {
				a.editorView(c, wide, panel)
			})
			ui.Column(c.Key("execution")).Width(280).MinHeight(0).Padding(10).Gap(8).
				Background(panel).Border(1, theme.Border).Radius(5).Children(func() {
				a.runView(c)
			})
		})
		ui.Modal(c.Key("files-dialog"), &a.filesOpen, func() {
			ui.Column(c).Width(355).Height(360).Padding(12).Gap(8).Children(func() {
				a.filesView(c)
				if ui.Button(c, "关闭").Clicked() {
					a.filesOpen = false
				}
			})
		})
		a.pickerView(c)
		ui.Modal(c.Key("model-dialog"), &a.modelOpen, func() {
			ui.Column(c).Width(460).Padding(14).Gap(8).Children(func() {
				ui.Text(c, "模型设置").FontSize(18).Bold()
				a.modelView(c)
				if ui.Button(c, "关闭").Clicked() {
					a.modelOpen = false
					a.secret = ""
				}
			})
		})
	})
	if !a.modelOpen {
		a.secret = ""
	}
	if !a.picker.open && !a.filesOpen && !a.modelOpen {
		switch {
		case c.Shortcut(ui.Ctrl, ui.KeyO):
			a.pickAsset()
		case a.doc != nil && c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyS):
			a.saveAsset(true)
		case a.doc != nil && c.Shortcut(ui.Ctrl, ui.KeyS):
			a.saveAsset(false)
		}
	}
}

func (a *desktop) filesView(c *ui.Context) {
	ui.Text(c, "用例与套件").FontSize(16).Bold()
	if ui.Button(c, "打开 YAML").FillWidth().Tooltip("Ctrl+O").Clicked() {
		a.pickAsset()
	}
	ui.Row(c).Gap(4).Children(func() {
		if ui.Button(c, "新建用例").Padding(6, 4).Grow(1).Clicked() {
			a.newAsset(false)
		}
		if ui.Button(c, "新建套件").Padding(6, 4).Grow(1).Clicked() {
			a.newAsset(true)
		}
	})
	ui.Scroll(c.Key("opened-files")).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Column(c).Role(ui.RoleListBox).Label("已打开的文件").FillWidth().Gap(4).Children(func() {
			for _, group := range []struct {
				label string
				suite bool
			}{{"用例", false}, {"套件", true}} {
				showHeading := true
				for _, doc := range a.documents {
					if group.suite != doc.Suite() {
						continue
					}
					if showHeading {
						ui.Text(c.Key(group.label), group.label).FontSize(12).TextColor(c.Theme().TextMuted)
						showHeading = false
					}
					title := documentTitle(doc)
					if doc.Dirty() {
						title += " · 未保存"
					}
					if selectionRow(c.Key(doc.id), title, doc == a.doc).Tooltip(doc.Path()).Clicked() {
						a.selectDocument(doc)
					}
				}
			}
		})
	})
}

func selectionRow(c *ui.Context, title string, selected bool) ui.Element {
	t := c.Theme()
	row := ui.ButtonBase(c).Role(ui.RoleOption).Checked(selected).Label(title).
		FillWidth().Padding(6, 8).Radius(t.Radius).Justify(ui.Start)
	switch {
	case selected:
		row.Background(t.Selection)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() { ui.Text(c, title).SingleLine().Grow(1).MinWidth(0) })
	return row
}

func (a *desktop) editorView(c *ui.Context, wide bool, panel ui.Color) {
	doc := a.doc
	if doc == nil {
		ui.Text(c, "开始编写测试").FontSize(23).Bold()
		ui.Text(c, "打开已有文件，或新建用例来编写第一个步骤。").Wrap()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "打开文件…").Clicked() {
				a.pickAsset()
			}
			if ui.Button(c, "创建用例").Clicked() {
				a.newAsset(false)
			}
		})
		a.editorMessage(c)
		return
	}
	ui.Text(c.Key("document-title"), documentTitle(doc)).FontSize(23).Bold().Wrap()
	label := doc.Path()
	if label == "" {
		label = "未保存到磁盘"
	} else if doc.Dirty() {
		label += " · 未保存"
	}
	ui.Text(c.Key("document-path"), label).FontSize(12).SingleLine().TruncateMiddle().Tooltip(label).TextColor(c.Theme().TextMuted)
	ui.Row(c.Key("document-actions")).Gap(5).Children(func() {
		if ui.Button(c, "保存").Tooltip("Ctrl+S").Clicked() {
			a.saveAsset(false)
		}
		if !doc.Suite() && ui.Button(c.Key("pick-live"), "从设备选元素").Clicked() {
			a.openLivePicker(doc)
		}
		ui.MenuButton(c, "文件操作", func(m *ui.Menu) {
			if m.Item("另存为…").Chosen() {
				a.saveAsset(true)
			}
			if m.Item("重新载入").Disabled(doc.Path() == "").Chosen() {
				a.reloadAsset()
			}
			m.Separator()
			if m.Item("关闭文件").Chosen() {
				a.closeDocument()
			}
		})
	})
	if a.doc != doc {
		return
	}
	if !doc.Suite() {
		a.reviewStepView(c, doc)
	}
	a.editorMessage(c)
	theme := c.Theme()
	contents := c.Key(doc.id)
	if wide {
		ui.Row(contents).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Gap(8).Children(func() {
			ui.Column(c.Key("steps")).Width(188).MinHeight(0).Gap(5).Children(func() { a.editRows(c) })
			ui.Scroll(c.Key("fields")).Grow(1).MinWidth(0).MinHeight(0).
				Background(panel).Border(1, theme.Border).Radius(5).Children(func() { a.inspector(c) })
		})
		return
	}
	ui.Column(contents).Grow(1).MinHeight(0).Gap(8).Children(func() {
		ui.Column(c.Key("steps")).Grow(1).MinHeight(0).Gap(5).Children(func() { a.editRows(c) })
		ui.Scroll(c.Key("fields")).Grow(2).MinHeight(0).FillWidth().
			Background(panel).Border(1, theme.Border).Radius(5).Children(func() { a.inspector(c) })
	})
}

func (a *desktop) editorMessage(c *ui.Context) {
	if a.problem != "" {
		ui.Text(c.Key("editor-error"), a.problem).Wrap().TextColor(c.Theme().Danger)
	} else if a.message != "" {
		ui.Text(c.Key("editor-message"), a.message).Wrap().TextColor(c.Theme().TextMuted)
	}
}

func (a *desktop) inspector(c *ui.Context) {
	doc := a.doc
	ui.Column(c).FillWidth().Padding(9).Gap(8).Children(func() {
		if !doc.Suite() {
			ui.Text(c, "用例名称").Bold()
			ui.Row(c).Gap(5).Children(func() {
				name := doc.Name()
				if ui.TextInput(c.Key("case-name"), &name).Label("用例名称").Placeholder("使用文件名").Grow(1).MinWidth(0).Changed() {
					doc.SetName(name)
				}
				if doc.HasName() && ui.Button(c, "使用文件名").Clicked() {
					doc.ClearName()
				} else if !doc.HasName() && ui.Button(c, "设为空名称").Clicked() {
					doc.SetName("")
				}
			})
		}
		if doc.row < 0 || doc.row >= doc.Rows() {
			hint := "选择列表中的条目查看详情。"
			if doc.Rows() == 0 {
				hint = "点击「添加步骤」编写第一步，也可以从设备选择元素。"
				if doc.Suite() {
					hint = "点击「添加用例」，按执行顺序加入用例文件。"
				}
			}
			ui.Text(c, hint).FontSize(12).Wrap()
			return
		}
		ui.Divider(c)
		ui.Text(c, fmt.Sprintf("%d. %s", doc.row+1, rowTitle(doc, doc.row))).Bold().Wrap()
		if doc.Suite() {
			ref := doc.Reference(doc.row)
			ui.Text(c, ref).Wrap()
			if doc.Path() != "" && ui.Button(c, "打开用例").Clicked() {
				a.loadAsset(filepath.Join(filepath.Dir(doc.Path()), filepath.FromSlash(ref)))
			}
		} else {
			a.stepFields(c)
		}
		a.editRowActions(c, doc)
	})
}

func rowTitle(doc *openedDocument, index int) string {
	if doc.Suite() {
		return doc.Reference(index)
	}
	if name, _ := doc.Field(index, assetform.StepName); name != "" {
		return name
	}
	return stepLabel(doc.StepKind(index))
}

func (a *desktop) editRows(c *ui.Context) {
	doc := a.doc
	label := "添加步骤"
	title := "步骤"
	if doc.Suite() {
		label = "添加用例"
		title = "用例顺序"
	}
	ui.Row(c.Key("step-heading")).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%s · %d", title, doc.Rows())).Bold().Grow(1)
		if ui.Button(c, label).Clicked() {
			if doc.Suite() {
				a.addReferences()
			} else {
				doc.AddStep()
				doc.row = doc.Rows() - 1
			}
			doc.focusRow = true
		}
	})
	focus := doc.focusRow
	doc.focusRow = false
	location := a.liveStep()
	ui.Scroll(c.Key("step-list")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Role(ui.RoleListBox).Label(title).Gap(3).FillWidth().Children(func() {
			for i := 0; i < doc.Rows(); i++ {
				text := fmt.Sprintf("%d  %s", i+1, rowTitle(doc, i))
				if i == location {
					text += " · 本次执行位置"
				}
				button := selectionRow(c.Key(i), text, doc.row == i).Tooltip(text)
				if focus && doc.row == i {
					button.Focus().ScrollIntoView()
				}
				switch {
				case button.Clicked():
					doc.row = i
				case button.Shortcut(0, ui.KeyUp) && i > 0:
					doc.row, doc.focusRow = i-1, true
				case button.Shortcut(0, ui.KeyDown) && i+1 < doc.Rows():
					doc.row, doc.focusRow = i+1, true
				}
			}
		})
	})
}

func (a *desktop) editRowActions(c *ui.Context, doc *openedDocument) {
	ui.Row(c.Key("row-actions")).Gap(6).Children(func() {
		if ui.Button(c, "上移").Disabled(doc.row <= 0).Clicked() {
			doc.MoveRow(doc.row, -1)
			doc.row--
			doc.focusRow = true
		}
		if ui.Button(c, "下移").Disabled(doc.row < 0 || doc.row >= doc.Rows()-1).Clicked() {
			doc.MoveRow(doc.row, 1)
			doc.row++
			doc.focusRow = true
		}
		if ui.Button(c, "移除").Disabled(doc.row < 0).Clicked() {
			doc.RemoveRow(doc.row)
			doc.row = min(doc.row, doc.Rows()-1)
			doc.focusRow = true
		}
	})
}

func (a *desktop) stepFields(c *ui.Context) {
	row := a.doc.row
	ui.Column(c.Key(row)).FillWidth().Gap(8).Children(func() {
		kind := a.doc.StepKind(row)
		selected := stepLabel(kind)
		kinds := make([]string, len(assetform.StepKinds))
		for i, value := range assetform.StepKinds {
			kinds[i] = stepLabel(value)
		}
		ui.Text(c, "步骤类型").Bold()
		ui.Select(c.Key("kind"), &selected, kinds).Label("步骤类型").FillWidth()
		if index := slices.Index(kinds, selected); index >= 0 && assetform.StepKinds[index] != kind {
			kind = assetform.StepKinds[index]
			a.doc.SetStepKind(row, kind)
		}
		check := a.doc.CheckKind(row)
		if kind == "precondition" || kind == "expect" {
			ui.Text(c, "检查方式").Bold()
			next := check
			ui.Select(c.Key("check"), &next, assetform.CheckKinds).Label("检查方式").FillWidth()
			if next != check {
				check = next
				a.doc.SetCheckKind(row, check)
			}
		}
		for _, field := range assetform.Fields(kind, check) {
			if a.doc.FieldVisible(row, field) {
				a.renderField(c, row, kind, check, field)
			}
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
			ui.Select(c, &next, field.Choices).Label(field.Label).Grow(1).MinWidth(0)
		} else {
			ui.TextInput(c, &next).Label(field.Label).Placeholder("未设置").Grow(1).MinWidth(0)
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

func (a *desktop) modelView(c *ui.Context) {
	if a.modelProblem != "" {
		ui.Text(c, a.modelProblem).Wrap().TextColor(c.Theme().Danger)
	} else if a.modelNote != "" {
		ui.Text(c, a.modelNote).Wrap().TextColor(c.Theme().TextMuted)
	}
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
