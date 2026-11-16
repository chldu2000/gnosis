package gnosis_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gnosis"
)

func TestLoad_caseNameStepOrderAndLocatorMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "sign-in.yaml")

	got, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Sign in",
		Steps: []gnosis.Step{
			{
				Name: "Tap the button",
				Locator: gnosis.Locator{
					ID:    "login",
					Text:  "Sign in",
					Index: 1,
				},
			},
			{
				Locator: gnosis.Locator{
					Description: "Password",
					Type:        "EditText",
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_omittedCaseNameUsesFilenameWithoutLastExtension(t *testing.T) {
	path := filepath.Join("testdata", "sign-in.case.yaml")

	got, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "sign-in.case" {
		t.Fatalf("Load(%q).Name = %q, want %q", path, got.Name, "sign-in.case")
	}
}

func TestLoad_twoFilesCanUseTheSameCaseName(t *testing.T) {
	paths := []string{
		filepath.Join("testdata", "shared-name", "a", "login.yaml"),
		filepath.Join("testdata", "shared-name", "b", "login.yaml"),
	}
	for _, path := range paths {
		got, err := gnosis.Load(path)
		if err != nil {
			t.Fatalf("Load(%q): %v", path, err)
		}
		if got.Name != "Sign in" {
			t.Fatalf("Load(%q).Name = %q, want %q", path, got.Name, "Sign in")
		}
	}
}

func TestLoad_caseWithNoSteps(t *testing.T) {
	paths := []string{
		filepath.Join("testdata", "empty-steps.yaml"),
		filepath.Join("testdata", "omitted-steps.yaml"),
	}
	for _, path := range paths {
		got, err := gnosis.Load(path)
		if err != nil {
			t.Fatalf("Load(%q): %v", path, err)
		}
		if got.Name != "Draft" || len(got.Steps) != 0 {
			t.Fatalf("Load(%q) = %+v, want name Draft and no steps", path, got)
		}
	}
}

func TestLoad_rejectsUnknownFieldAtItsPosition(t *testing.T) {
	path := filepath.Join("testdata", "unknown-field.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   2,
		Column: 1,
		Reason: `unknown field "extra"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsTapWithoutLocator(t *testing.T) {
	path := filepath.Join("testdata", "missing-locator.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing locator",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsWrongTypeAtItsPosition(t *testing.T) {
	path := filepath.Join("testdata", "wrong-type.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   5,
		Column: 13,
		Reason: `field "id" must be a string`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsIndexThatIsNotAnInteger(t *testing.T) {
	path := filepath.Join("testdata", "index-wrong-type.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 16,
		Reason: `field "index" must be an integer`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsDuplicateKeyAtItsPosition(t *testing.T) {
	path := filepath.Join("testdata", "duplicate-key.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   2,
		Column: 1,
		Reason: `duplicate key "name"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsLocatorThatOnlyHasIndex(t *testing.T) {
	path := filepath.Join("testdata", "index-only.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 7,
		Reason: "locator requires id, text, description, or type",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsIndexBelowOne(t *testing.T) {
	path := filepath.Join("testdata", "index-zero.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 16,
		Reason: "index must be >= 1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsStepWithoutTap(t *testing.T) {
	path := filepath.Join("testdata", "missing-tap.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing tap",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsInvalidUTF8AtItsPosition(t *testing.T) {
	path := filepath.Join("testdata", "bad-utf8.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   1,
		Column: 10,
		Reason: "file is not valid UTF-8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsNegativeIndex(t *testing.T) {
	path := filepath.Join("testdata", "index-negative.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 16,
		Reason: "index must be >= 1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_keepsAnEmptyCaseName(t *testing.T) {
	path := filepath.Join("testdata", "empty-name.yaml")

	got, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "" {
		t.Fatalf("Load(%q).Name = %q, want an empty name", path, got.Name)
	}
}

func TestLoad_keepsAnEmptyLocatorID(t *testing.T) {
	path := filepath.Join("testdata", "empty-id.yaml")

	got, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := gnosis.Case{
		Name: "Sign in",
		Steps: []gnosis.Step{{
			Locator: gnosis.Locator{ID: ""},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsStepsThatAreNotASequence(t *testing.T) {
	path := filepath.Join("testdata", "steps-wrong-type.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   2,
		Column: 8,
		Reason: "steps must be a sequence",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsCaseThatIsNotAMapping(t *testing.T) {
	path := filepath.Join("testdata", "not-a-mapping.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   1,
		Column: 1,
		Reason: "case must be a mapping",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsDuplicateLocatorKey(t *testing.T) {
	path := filepath.Join("testdata", "duplicate-locator-key.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 9,
		Reason: `duplicate key "id"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_doesNotModifyTheFile(t *testing.T) {
	paths := []string{
		filepath.Join("testdata", "sign-in.yaml"),
		filepath.Join("testdata", "unknown-field.yaml"),
		filepath.Join("testdata", "bad-utf8.yaml"),
	}
	for _, path := range paths {
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		_, _ = gnosis.Load(path)
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("Load(%q) modified the file", path)
		}
	}
}

func TestLoad_rejectsBrokenYAMLAtItsLine(t *testing.T) {
	path := filepath.Join("testdata", "broken.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   1,
		Column: 1,
		Reason: "did not find expected node content",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsCommentOnlyFile(t *testing.T) {
	path := filepath.Join("testdata", "comment-only.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   1,
		Column: 1,
		Reason: "case must be a mapping",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsMissingFile(t *testing.T) {
	path := filepath.Join("testdata", "missing-case.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   0,
		Column: 0,
		Reason: "cannot read file",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsASecondDocument(t *testing.T) {
	path := filepath.Join("testdata", "two-documents.yaml")

	_, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 1,
		Reason: "file has more than one document",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}
