package gnosis

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gnosis/internal/fileio"
	"gopkg.in/yaml.v3"
)

// WithCaseDraft keeps review material alongside the report of a standalone trial.
func WithCaseDraft(source DraftSource) RunOption {
	return func(s *runSettings) {
		if source.data != nil {
			s.draftSource = &source
		}
	}
}

type DraftTurn struct {
	Row                     int // zero-based row in the generated document
	Step, Turn              int // one-based positions in the source execution
	Goal, Note              string
	Coordinate              bool
	Before                  *InterfaceCapture `json:",omitempty"`
	BeforeImage, AfterImage string
}

type CaseDraft struct {
	YAML        string
	Unavailable string      `json:",omitempty"`
	Turns       []DraftTurn `json:",omitempty"`
	Expected    []string    `json:",omitempty"`
}

// RecordedTap reads the original action even after the editor has replaced or moved its row.
func (d *CaseDraft) RecordedTap(row int) *TapAt {
	var source struct {
		Steps []struct {
			Tap *TapAt `yaml:"tap_at"`
		} `yaml:"steps"`
	}
	if row < 0 || yaml.Unmarshal([]byte(d.YAML), &source) != nil || row >= len(source.Steps) {
		return nil
	}
	return source.Steps[row].Tap
}

func buildDraft(source DraftSource, record *RunRecord, attachments *caseAttachments, view reportCase) (*CaseDraft, error) {
	refuse := func(reason string) (*CaseDraft, error) { return &CaseDraft{Unavailable: reason}, nil }
	if record.Result != ResultPass || (record.State != "" && record.State != ExecutionFinished) {
		return refuse("需要已结束且通过的单用例执行")
	}
	root, err := parseFile(source.path, source.data)
	if err != nil {
		return nil, err
	}
	var rows *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "steps" {
			rows = root.Content[i+1]
			break
		}
	}
	if rows == nil {
		return refuse("来源用例没有 AI 步骤")
	}
	if len(rows.Content) != len(record.Steps) {
		return refuse("执行记录与来源步骤数不符")
	}
	for i, row := range rows.Content {
		for j := 0; j < len(row.Content); j += 2 {
			if row.Content[j].Value == "ai" && len(record.Steps[i].Trajectory) == 0 {
				return refuse(fmt.Sprintf("第 %d 个步骤没有留下动作", i+1))
			}
		}
	}
	var draft CaseDraft
	var output []*yaml.Node
	for i, row := range rows.Content {
		var ai *yaml.Node
		for j := 0; j < len(row.Content); j += 2 {
			if row.Content[j].Value == "expect" {
				data, err := yaml.Marshal(row)
				if err != nil {
					return nil, err
				}
				draft.Expected = append(draft.Expected, strings.TrimSpace(string(data)))
			}
			if row.Content[j].Value == "ai" {
				ai = row.Content[j+1]
				break
			}
		}
		if ai == nil {
			output = append(output, row)
			continue
		}
		var cfg AI
		if err := ai.Decode(&cfg); err != nil {
			return nil, err
		}
		trajectory := record.Steps[i].Trajectory
		for j, turn := range trajectory {
			if turn.row == nil {
				return refuse("执行记录缺少动作参数")
			}
			review := DraftTurn{Row: len(output), Step: i + 1, Turn: j + 1, Goal: cfg.Goal, Note: turn.Note, Coordinate: turn.Action.TapAt != nil, AfterImage: view.Steps[i].Trajectory[j].Image}
			if turn.before != nil {
				capture := *turn.before
				path, err := attachments.saveFrame(i+1, j+1, "-before", capture.Screenshot)
				if err != nil {
					return nil, err
				}
				capture.Screenshot = nil
				review.Before, review.BeforeImage = &capture, path
			}
			draft.Turns = append(draft.Turns, review)
			output = append(output, turn.row)
		}
	}
	if len(draft.Turns) == 0 {
		return refuse("来源用例没有 AI 步骤")
	}
	rows.Content = output
	data, err := yaml.Marshal(root)
	if err != nil {
		return nil, err
	}
	if err := ValidateAsset(source.path, data); err != nil {
		return nil, err
	}
	draft.YAML = string(data)
	return &draft, nil
}

const draftFilename = "case-draft.json"

// ReadCaseDraft opens the review associated with this execution report.
func ReadCaseDraft(report string) (*CaseDraft, error) {
	if report == "" {
		return nil, errors.New("没有可复核的单用例报告")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(report), draftFilename))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("本次执行未保存草稿材料")
	}
	if err != nil {
		return nil, err
	}
	var d CaseDraft
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if d.Unavailable != "" {
		return nil, errors.New(d.Unavailable)
	}
	if strings.TrimSpace(d.YAML) == "" {
		return nil, errors.New("草稿内容为空")
	}
	return &d, nil
}

func (d *CaseDraft) SaveNew(path string) error {
	if err := ValidateAsset(path, []byte(d.YAML)); err != nil {
		return err
	}
	return fileio.Create(path, []byte(d.YAML))
}

func (r *sessionReport) writeDraft(record *RunRecord, item reportCase, files *caseAttachments) error {
	if r.secret != "" && bytes.Contains(r.source.data, []byte(r.secret)) {
		return saveDraft(files.directory, &CaseDraft{Unavailable: "来源用例包含模型凭据"})
	}
	draft, err := buildDraft(*r.source, record, files, item)
	if err != nil {
		return err
	}
	if r.secret != "" {
		for _, turn := range draft.Turns {
			if turn.Before != nil {
				turn.Before.Elements = slices.Clone(turn.Before.Elements)
				for i := range turn.Before.Elements {
					e := &turn.Before.Elements[i]
					e.ID, e.Text, e.Description, e.Type = r.text(e.ID), r.text(e.Text), r.text(e.Description), r.text(e.Type)
				}
			}
		}
	}
	return saveDraft(files.directory, draft)
}

func saveDraft(directory string, draft *CaseDraft) error {
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return fileio.Replace(filepath.Join(directory, draftFilename), data)
}
