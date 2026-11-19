package main

import (
	"errors"
	"path/filepath"

	"github.com/egoist/mygo"
	"gnosis/internal/assetform"
	"gnosis/internal/localrun"
)

var yamlFilters = []mygo.FileFilter{{Name: "Gnosis YAML", Extensions: []string{"yaml", "yml"}}}

func (a *desktop) canDiscard() bool {
	return a.doc == nil || !a.doc.Dirty() || a.confirm("未保存的编辑", "当前文件有未保存的修改。继续将丢弃这些修改。", "放弃修改")
}

func (a *desktop) loadAsset(path string) {
	doc, err := assetform.Open(path)
	if err != nil {
		a.showError(err)
		return
	}
	a.doc, a.row, a.page = doc, -1, "edit"
	a.problem, a.message = "", ""
	if doc.Rows() > 0 {
		a.row = 0
	}
	if err := doc.Validate(); err != nil {
		a.showError(err)
	}
}

func (a *desktop) pickAsset() {
	if !a.canDiscard() {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "打开用例或套件", Filters: yamlFilters})
	if err != nil {
		a.showError(err)
	} else if len(paths) > 0 {
		a.loadAsset(paths[0])
	}
}

func (a *desktop) newAsset(suite bool) {
	if a.canDiscard() {
		a.doc, a.row, a.page = assetform.New(suite), -1, "edit"
		a.problem, a.message = "", ""
	}
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
	if err := a.doc.Save(path); err != nil {
		if errors.Is(err, assetform.ErrChangedOnDisk) {
			a.problem, a.message = "文件在打开后已被修改。请重新载入磁盘文件，或将当前编辑另存为。", ""
			a.page = "edit"
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
		a.row = a.doc.Rows() - 1
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
		a.showError(err)
		return
	}
	if key != nil {
		a.model.HasKey = *key != ""
	}
	a.secret, a.problem, a.message = "", "", "模型设置已保存"
}
