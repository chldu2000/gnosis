package gnosis_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gnosis"
)

func TestLoad_preservesEmptyLocatorConditions(t *testing.T) {
	got, _, err := gnosis.Load(filepath.Join("testdata", "empty-locator-conditions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		field         string
		without, with int
	}{
		{"text", 0, 1},
		{"description", 0, 2},
		{"type", 0, 3},
		{"id", 4, 5},
	} {
		t.Run(pair.field, func(t *testing.T) {
			if reflect.DeepEqual(got.Steps[pair.without].Locator, got.Steps[pair.with].Locator) {
				t.Fatalf("omitted %s and explicitly empty %s have the same locator", pair.field, pair.field)
			}
		})
	}
}

func TestLoad_rejectsTaggedContainers(t *testing.T) {
	for _, test := range []struct {
		file, reason string
		line, column int
	}{
		{"tagged-name.yaml", `field "name" must be a string`, 1, 7},
		{"tagged-input-text.yaml", `field "text" must be a string`, 3, 13},
		{"tagged-locator.yaml", `field "text" must be a string`, 4, 15},
		{"tagged-screen-text.yaml", `field "text" must be a string`, 4, 15},
	} {
		t.Run(test.file, func(t *testing.T) {
			path := filepath.Join("testdata", test.file)
			got, suite, err := gnosis.Load(path)
			var rejection *gnosis.Rejection
			if !errors.As(err, &rejection) {
				t.Fatalf("Load accepted a container as a string: case=%+v, error=%v", got, err)
			}
			want := &gnosis.Rejection{File: path, Line: test.line, Column: test.column, Reason: test.reason}
			if !reflect.DeepEqual(rejection, want) || suite != nil || !reflect.DeepEqual(got, gnosis.Case{}) {
				t.Fatalf("Load = (%+v, %+v, %+v), want empty results and %+v", got, suite, rejection, want)
			}
		})
	}
}

func TestLoad_decodesYAMLNumbers(t *testing.T) {
	got, _, err := gnosis.Load(filepath.Join("testdata", "yaml-numbers.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Steps[0].Locator.Index != 8 || got.Steps[1].AI.MaxSteps != 8 || got.Steps[2].AI.MaxSteps != 16 {
		t.Fatalf("integer values changed: index=%d, max_steps=%d/%d", got.Steps[0].Locator.Index, got.Steps[1].AI.MaxSteps, got.Steps[2].AI.MaxSteps)
	}
	if *got.Steps[3].TapAt != (gnosis.TapAt{X: 16, Y: 8}) {
		t.Fatalf("tap coordinates = %+v, want 16/8", got.Steps[3].TapAt)
	}
	swipe := got.Steps[4].Swipe
	if swipe.FromX != 8 || swipe.FromY != 2 || swipe.ToX != 1000 || swipe.ToY != -0.5 {
		t.Fatalf("swipe coordinates = %+v, want 8/2/1000/-0.5", swipe)
	}
}

func TestLoad_rejectsReferencedSuites(t *testing.T) {
	for _, test := range []struct {
		name, target, body string
	}{
		{"nested", "nested.yaml", "cases: [missing.yaml]\n"},
		{"self", "suite.yaml", ""},
		{"mutual", "nested.yaml", "cases: [suite.yaml]\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "suite.yaml")
			casePath := filepath.Join(dir, test.target)
			files := map[string]string{
				path:                             "cases:\n  - valid.yaml\n  - " + test.target + "\n",
				filepath.Join(dir, "valid.yaml"): "name: Valid\n",
			}
			if test.body != "" {
				files[casePath] = test.body
			}
			for file, body := range files {
				if err := os.WriteFile(file, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, suite, err := gnosis.Load(path)
			var rejection *gnosis.Rejection
			if !errors.As(err, &rejection) {
				t.Fatalf("Load returned %v, want rejection", err)
			}
			want := &gnosis.Rejection{
				File: path, Line: 3, Column: 5, Reference: 2, CaseFile: casePath,
				CaseLine: 1, CaseColumn: 1, Reason: "referenced file is a suite",
			}
			if !reflect.DeepEqual(rejection, want) || suite != nil || !reflect.DeepEqual(got, gnosis.Case{}) {
				t.Fatalf("Load = (%+v, %+v, %+v), want empty results and %+v", got, suite, rejection, want)
			}
			for file, body := range files {
				after, err := os.ReadFile(file)
				if err != nil || string(after) != body {
					t.Fatalf("Load modified %s", file)
				}
			}
		})
	}
}
