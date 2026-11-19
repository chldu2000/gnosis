package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"gnosis/internal/assetform"
	"gnosis/internal/localrun"
)

var yamlFilters = []mygo.FileFilter{{Name: "Gnosis YAML", Extensions: []string{"yaml", "yml"}}}

type openedDocument struct {
	*assetform.Document
	id       int
	row      int
	focusRow bool
}

func documentTitle(doc *openedDocument) string {
	if doc.Suite() {
		if doc.Path() == "" {
			return "未命名套件"
		}
	} else if doc.Name() != "" {
		return doc.Name()
	}
	if doc.Path() != "" {
		return filepath.Base(doc.Path())
	}
	return "未命名用例"
}

func (a *desktop) canDiscard(doc *openedDocument) bool {
	return !doc.Dirty() || a.confirm("未保存的编辑", "「"+documentTitle(doc)+"」有未保存的修改。", "放弃修改")
}

func (a *desktop) canCloseWindow() bool {
	var dirty []string
	for _, doc := range a.documents {
		if doc.Dirty() {
			dirty = append(dirty, documentTitle(doc))
		}
	}
	return len(dirty) == 0 || a.confirm("未保存的编辑", "以下文件有未保存的修改：\n"+strings.Join(dirty, "\n"), "放弃修改并关闭")
}

func (a *desktop) findDocument(path string) *openedDocument {
	for _, doc := range a.documents {
		if doc.RefersTo(path) {
			return doc
		}
	}
	return nil
}

func (a *desktop) selectDocument(doc *openedDocument) {
	if a.doc != doc {
		a.doc = doc
		a.problem, a.message = "", ""
	}
	a.filesOpen = false
}

func (a *desktop) addDocument(doc *assetform.Document) *openedDocument {
	a.nextDocID++
	open := &openedDocument{Document: doc, id: a.nextDocID, row: -1}
	if doc.Rows() > 0 {
		open.row = 0
	}
	a.documents = append(a.documents, open)
	a.selectDocument(open)
	return open
}

func (a *desktop) loadAsset(path string) *openedDocument {
	path, err := filepath.Abs(path)
	if err != nil {
		a.showError(err)
		return nil
	}
	if doc := a.findDocument(path); doc != nil {
		a.selectDocument(doc)
		return doc
	}
	doc, err := assetform.Open(path)
	if err != nil {
		a.showError(err)
		return nil
	}
	open := a.addDocument(doc)
	if err := doc.Validate(); err != nil {
		a.showError(err)
	}
	return open
}

func (a *desktop) reloadAsset() {
	open := a.doc
	if open == nil || open.Path() == "" || !a.canDiscard(open) {
		return
	}
	doc, err := assetform.Open(open.Path())
	if err != nil {
		a.showError(err)
		return
	}
	open.Document = doc
	open.row = min(max(0, open.row), doc.Rows()-1)
	a.problem, a.message = "", "已重新载入"
	if err := doc.Validate(); err != nil {
		a.showError(err)
	}
}

func (a *desktop) closeDocument() {
	doc := a.doc
	if doc == nil || !a.canDiscard(doc) {
		return
	}
	index := slices.Index(a.documents, doc)
	a.documents = slices.Delete(a.documents, index, index+1)
	a.selectDocument(nil)
	if len(a.documents) > 0 {
		a.selectDocument(a.documents[min(index, len(a.documents)-1)])
	}
}

func (a *desktop) pickAsset() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "打开用例或套件", Filters: yamlFilters, Multiple: true})
	if err != nil {
		a.showError(err)
		return
	}
	for _, path := range paths {
		if a.loadAsset(path) == nil {
			return
		}
	}
}

func (a *desktop) newAsset(suite bool) {
	a.addDocument(assetform.New(suite))
}

func (a *desktop) saveAsset(saveAs bool) bool {
	if a.doc == nil {
		return false
	}
	path := a.doc.Path()
	if saveAs || path == "" {
		if path == "" {
			path = "case.yaml"
			if a.doc.Suite() {
				path = "suite.yaml"
			}
		}
		var err error
		path, err = mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.window, Title: "保存 YAML 文件", DefaultPath: path, Filters: yamlFilters})
		if err != nil {
			a.showError(err)
			return false
		}
		if path == "" {
			return false
		}
		if filepath.Ext(path) == "" {
			path += ".yaml"
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		a.showError(err)
		return false
	}
	if other := a.findDocument(path); other != nil && other != a.doc {
		a.showError(fmt.Errorf("「%s」已在工作台打开。请为另存的文件选择其他路径", path))
		return false
	}
	if err := a.doc.Save(path); err != nil {
		if errors.Is(err, assetform.ErrChangedOnDisk) {
			a.problem, a.message = "文件在打开后已被修改。请重新载入磁盘文件，或将当前编辑另存为。", ""
		} else {
			a.showError(err)
		}
		return false
	}
	a.problem, a.message = "", "文件已保存"
	return true
}

func (a *desktop) addReferences() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.window, Title: "选择用例文件", Filters: yamlFilters, Multiple: true,
	})
	if err != nil {
		a.showError(err)
		return
	}
	for _, path := range paths {
		if err := a.doc.AddReference(path); err != nil {
			a.showError(err)
			return
		}
		a.doc.row = a.doc.Rows() - 1
	}
}

func (a *desktop) storeModel(removeKey bool) {
	settings := a.model
	var key *string
	if removeKey {
		empty := ""
		key = &empty
	} else if a.secret != "" {
		value := a.secret
		key = &value
	}
	if err := localrun.SaveModel(settings, key); err != nil {
		a.modelProblem, a.modelNote = err.Error(), ""
		return
	}
	if key != nil {
		a.model.HasKey = *key != ""
	}
	a.secret, a.modelProblem, a.modelNote = "", "", "模型设置已保存"
}
