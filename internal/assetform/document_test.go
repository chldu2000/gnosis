package assetform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gnosis"
)

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFormEditPreservesCommentsAndImplicitDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.yaml")
	put(t, path, "# case comment\nname: Original\nsteps:\n  - name: Rename me\n    long_press:\n      locator: # target comment\n        id: confirm\n  # explanation\n  - expect:\n      exists: {id: next} # still implicit\n")
	doc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.SetField(0, StepName, "Hold confirm")
	if err := doc.Save(path); err != nil {
		t.Fatal(err)
	}
	output := read(t, path)
	for _, comment := range []string{"# case comment", "# target comment", "# explanation", "# still implicit"} {
		if !strings.Contains(output, comment) {
			t.Fatalf("lost %s in %s", comment, output)
		}
	}
	if strings.Contains(output, "duration:") || strings.Contains(output, "timeout:") || doc.Dirty() {
		t.Fatalf("implicit defaults or dirty state changed: %s", output)
	}
	loaded, suite, err := gnosis.Load(path)
	if err != nil || suite != nil || loaded.Steps[0].Name != "Hold confirm" || loaded.Steps[1].Expect == nil {
		t.Fatalf("saved file does not load: %+v, %v", loaded, err)
	}
}

func TestFormRefusesOverwritingExternalChanges(t *testing.T) {
	dir := t.TempDir()
	path, copyTo := filepath.Join(dir, "case.yaml"), filepath.Join(dir, "copy.yaml")
	put(t, path, "name: Opened\n")
	doc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.SetName("Edited in form")
	external := "name: Edited externally\n"
	put(t, path, external)
	if err := doc.Save(path); !errors.Is(err, ErrChangedOnDisk) || !doc.Dirty() || read(t, path) != external {
		t.Fatalf("external edit not protected: %v", err)
	}
	if err := doc.Save(copyTo); err != nil || !strings.Contains(read(t, copyTo), "Edited in form") {
		t.Fatalf("form changes could not be saved separately: %v", err)
	}
}

func TestSuiteSaveAsRetainsReferenceTargetSequenceAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source", "suite.yaml")
	dest := filepath.Join(dir, "destination", "suite.yaml")
	caseA := filepath.Join(dir, "source", "a.yaml")
	caseB := filepath.Join(dir, "b.yaml")
	put(t, caseA, "name: A\n")
	put(t, caseB, "name: B\n")
	put(t, source, "cases:\n  - a.yaml # first\n  - ../b.yaml\n  - a.yaml # duplicate\n")
	doc, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(dest); err != nil {
		t.Fatal(err)
	}
	_, suite, err := gnosis.Load(dest)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, testCase := range suite.Cases {
		names = append(names, testCase.Name)
	}
	if !slices.Equal(names, []string{"A", "B", "A"}) || !strings.Contains(read(t, dest), "../source/a.yaml # duplicate") {
		t.Fatalf("rebase changed target or comment: %v; %s", names, read(t, dest))
	}
	if err := doc.Save(caseA); err == nil || read(t, caseA) != "name: A\n" {
		t.Fatalf("save as did not protect a referenced case: %v", err)
	}
}

func TestSuiteRefusesUnrepresentableRelativePathBeforeWriting(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows drive letters")
	}
	dir := t.TempDir()
	caseFile := filepath.Join(dir, "case.yaml")
	put(t, caseFile, "name: A\n")
	suite := New(true)
	if err := suite.AddReference(caseFile); err != nil {
		t.Fatal(err)
	}
	drive := "Z:"
	if strings.EqualFold(filepath.VolumeName(caseFile), drive) {
		drive = "Y:"
	}
	destination := drive + `\gnosis-cross-volume\suite.yaml`
	if err := suite.Save(destination); err == nil || !strings.Contains(err.Error(), "reference 1") || !suite.Dirty() || suite.Path() != "" {
		t.Fatalf("cross-volume save did not identify the reference: %v", err)
	}
}

func TestNewAssetFormsSaveUsingTheSharedLoader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.yaml")
	doc := New(false)
	for index, kind := range StepKinds {
		doc.AddStep()
		doc.SetStepKind(index, kind)
		check := ""
		if kind == "precondition" || kind == "expect" {
			check = "attr_equals"
			doc.SetCheckKind(index, check)
		}
		for _, f := range Fields(kind, check) {
			var value string
			switch f.Label {
			case "元素 ID":
				value = "button"
			case "应用标识":
				value = "com.example"
			case "等待时长":
				value = "0ms"
			case "目标":
				value = "read the screen"
			case "替换文本":
				value = ""
			default:
				if f.Optional {
					continue
				}
				if len(f.Choices) > 0 {
					value = f.Choices[0]
				} else {
					value = "0.5"
				}
			}
			doc.SetField(index, f, value)
		}
	}
	if err := doc.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := gnosis.Load(path)
	if err != nil || len(loaded.Steps) != len(StepKinds) {
		t.Fatalf("form output could not be read by the shared core: %v", err)
	}
	suite := New(true)
	if err := suite.AddReference(path); err != nil {
		t.Fatal(err)
	}
	if err := suite.Save(filepath.Join(dir, "suite.yaml")); err != nil {
		t.Fatal(err)
	}
	if suite.Reference(0) != "new.yaml" {
		t.Fatalf("new suite did not save as relative: %s", suite.Reference(0))
	}
}
