package gnosis_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gnosis"
)

func TestLoad_suiteLoadsCasesInFileOrder(t *testing.T) {
	path := filepath.Join("testdata", "suite", "suite.yaml")

	_, got, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load suite result is nil")
	}

	wantOpen := gnosis.Case{
		Name: "Open",
		Steps: []gnosis.Step{
			{Launch: &gnosis.Launch{App: "demo.app"}},
		},
	}
	wantSignIn := gnosis.Case{
		Name: "Sign in",
		Steps: []gnosis.Step{
			{
				Name: "Tap the button",
				Locator: gnosis.Locator{
					ID:    locatorString("login"),
					Text:  locatorString("Sign in"),
					Index: 1,
				},
			},
			{
				Locator: gnosis.Locator{
					Description: locatorString("Password"),
					Type:        locatorString("EditText"),
				},
			},
		},
	}
	want := []gnosis.Case{wantOpen, wantSignIn}
	if !reflect.DeepEqual(got.Cases, want) {
		t.Fatalf("Load(%q) cases = %#v, want %#v", path, got.Cases, want)
	}
}

func TestLoad_suiteReferenceTwiceIsTwoCases(t *testing.T) {
	path := filepath.Join("testdata", "suite", "twice.yaml")

	_, got, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Cases) != 2 {
		t.Fatalf("Load(%q) has %d cases, want 2", path, len(got.Cases))
	}
	if !reflect.DeepEqual(got.Cases[0], got.Cases[1]) {
		t.Fatalf("Load(%q) cases differ: %#v and %#v", path, got.Cases[0], got.Cases[1])
	}
	if got.Cases[0].Steps[0].Launch == got.Cases[1].Steps[0].Launch {
		t.Fatal("Load reused one launch value for both references")
	}
}

func TestLoad_rejectsEmptySuiteAbsolutePathAndUnknownField(t *testing.T) {
	dir := t.TempDir()
	absCase := filepath.Join(dir, "case.yaml")
	if err := os.WriteFile(absCase, []byte("name: Alone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	absSuite := filepath.Join(dir, "absolute.yaml")
	quoted := fmt.Sprintf("%q", filepath.ToSlash(absCase))
	if err := os.WriteFile(absSuite, []byte("cases:\n  - "+quoted+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want *gnosis.Rejection
	}{
		{
			name: "empty",
			path: filepath.Join("testdata", "suite", "empty.yaml"),
			want: &gnosis.Rejection{
				File:   filepath.Join("testdata", "suite", "empty.yaml"),
				Line:   1,
				Column: 8,
				Reason: "cases must not be empty",
			},
		},
		{
			name: "absolute",
			path: absSuite,
			want: &gnosis.Rejection{
				File:      absSuite,
				Line:      2,
				Column:    5,
				Reason:    "case path must be relative",
				Reference: 1,
				CaseFile:  filepath.ToSlash(absCase),
			},
		},
		{
			name: "unknown",
			path: filepath.Join("testdata", "suite", "unknown.yaml"),
			want: &gnosis.Rejection{
				File:   filepath.Join("testdata", "suite", "unknown.yaml"),
				Line:   1,
				Column: 1,
				Reason: `unknown field "title"`,
			},
		},
		{
			name: "blank",
			path: filepath.Join("testdata", "suite", "blank.yaml"),
			want: &gnosis.Rejection{
				File:      filepath.Join("testdata", "suite", "blank.yaml"),
				Line:      2,
				Column:    5,
				Reason:    "case path must not be empty",
				Reference: 1,
			},
		},
		{
			name: "mixed",
			path: filepath.Join("testdata", "suite", "mixed.yaml"),
			want: &gnosis.Rejection{
				File:   filepath.Join("testdata", "suite", "mixed.yaml"),
				Line:   2,
				Column: 1,
				Reason: "file mixes case and suite fields",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := gnosis.Load(test.path)
			var got *gnosis.Rejection
			if !errors.As(err, &got) {
				t.Fatalf("Load(%q) err = %v, want rejection", test.path, err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Load(%q) err = %+v, want %+v", test.path, got, test.want)
			}
		})
	}
}

func TestLoad_suiteStopsWhenAReferenceFails(t *testing.T) {
	missing := filepath.Join("testdata", "suite", "missing.yaml")
	bad := filepath.Join("testdata", "suite", "bad-ref.yaml")

	_, _, err := gnosis.Load(missing)
	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", missing, err)
	}
	wantMissing := &gnosis.Rejection{
		File:      missing,
		Line:      3,
		Column:    5,
		Reason:    "cannot read file",
		Reference: 2,
		CaseFile:  filepath.Join("testdata", "suite", "gone.yaml"),
	}
	if !reflect.DeepEqual(got, wantMissing) {
		t.Fatalf("Load(%q) err = %+v, want %+v", missing, got, wantMissing)
	}

	_, _, err = gnosis.Load(bad)
	got = nil
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", bad, err)
	}
	wantBad := &gnosis.Rejection{
		File:       bad,
		Line:       2,
		Column:     5,
		Reason:     `unknown field "extra"`,
		Reference:  1,
		CaseFile:   filepath.Join("testdata", "suite", "bad.yaml"),
		CaseLine:   2,
		CaseColumn: 1,
	}
	if !reflect.DeepEqual(got, wantBad) {
		t.Fatalf("Load(%q) err = %+v, want %+v", bad, got, wantBad)
	}
}

func TestLoad_suiteDoesNotModifyFiles(t *testing.T) {
	dir := filepath.Join("testdata", "suite")
	paths := []string{
		filepath.Join(dir, "suite.yaml"),
		filepath.Join(dir, "open.yaml"),
		filepath.Join("testdata", "sign-in.yaml"),
	}
	before := make([][]byte, len(paths))
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[i] = data
	}

	if _, _, err := gnosis.Load(paths[0]); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for i, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before[i], after) {
			t.Fatalf("Load modified %s", path)
		}
	}
}
