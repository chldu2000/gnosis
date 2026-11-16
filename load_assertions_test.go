package gnosis_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gnosis"
)

func TestLoad_rejectsAssertionWithTwoChecks(t *testing.T) {
	path := filepath.Join("testdata", "two-checks.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 7,
		Reason: "assertion has more than one check",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_checksPositionsActionsAndAIKeepFileOrder(t *testing.T) {
	path := filepath.Join("testdata", "assertions-and-ai.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Sign in",
		Steps: []gnosis.Step{
			{
				Name:   "Open",
				Launch: &gnosis.Launch{App: "com.example.mail"},
			},
			{
				Name: "Login is there",
				Precondition: &gnosis.Assertion{
					Timeout: 10 * time.Second,
					Exists: &gnosis.Locator{
						ID:   "login",
						Text: "Sign in",
					},
				},
			},
			{Locator: gnosis.Locator{ID: "login"}},
			{
				Expect: &gnosis.Assertion{
					Timeout: 2 * time.Second,
					TextEquals: &gnosis.TextCheck{
						Locator: gnosis.Locator{ID: "title"},
						Text:    "Welcome",
					},
				},
			},
			{
				Precondition: &gnosis.Assertion{
					Timeout: 0,
					Absent:  &gnosis.Locator{ID: "spinner"},
				},
			},
			{
				Expect: &gnosis.Assertion{
					Timeout: 10 * time.Second,
					TextContains: &gnosis.TextCheck{
						Locator: gnosis.Locator{Description: "Hint"},
						Text:    "saved",
					},
				},
			},
			{
				Precondition: &gnosis.Assertion{
					Timeout: 10 * time.Second,
					AttrEquals: &gnosis.AttrEquals{
						Locator:   gnosis.Locator{ID: "agree"},
						Attribute: "checked",
						Value:     true,
					},
				},
			},
			{
				Expect: &gnosis.Assertion{
					Timeout:    10 * time.Second,
					ScreenText: &gnosis.ScreenText{Text: "Done"},
				},
			},
			{
				Name: "Finish",
				AI: &gnosis.AI{
					Goal:     "Return to the inbox",
					MaxSteps: 4,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsMaxStepsThatIsNotAPositiveInteger(t *testing.T) {
	tests := []string{
		"ai-max-steps-zero.yaml",
		"ai-max-steps-negative.yaml",
		"ai-max-steps-string.yaml",
		"ai-max-steps-decimal.yaml",
	}
	for _, file := range tests {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join("testdata", file)

			_, _, err := gnosis.Load(path)

			var got *gnosis.Rejection
			if !errors.As(err, &got) {
				t.Fatalf("Load(%q) err = %v, want rejection", path, err)
			}
			want := &gnosis.Rejection{
				File:   path,
				Line:   5,
				Column: 18,
				Reason: `field "max_steps" must be a positive integer`,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
			}
		})
	}
}

func TestLoad_omittedMaxStepsIs20(t *testing.T) {
	path := filepath.Join("testdata", "ai-default-max-steps.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Ask",
		Steps: []gnosis.Step{{
			AI: &gnosis.AI{
				Goal:     "Open settings",
				MaxSteps: 20,
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_aiGoalAndMaxStepsMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "ai-step.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Ask",
		Steps: []gnosis.Step{{
			Name: "Open settings",
			AI: &gnosis.AI{
				Goal:     "Open the settings screen",
				MaxSteps: 8,
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsUnknownCheck(t *testing.T) {
	path := filepath.Join("testdata", "unknown-check.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 7,
		Reason: `unknown field "visible"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsAssertionWithoutPosition(t *testing.T) {
	path := filepath.Join("testdata", "assertion-missing-position.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: `unknown field "exists"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsStepWithTwoPositions(t *testing.T) {
	path := filepath.Join("testdata", "two-positions.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 5,
		Reason: "step has more than one action",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsAssertionWithoutACheck(t *testing.T) {
	path := filepath.Join("testdata", "assertion-missing-check.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing check",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_expectScreenTextMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "expect-screen-text.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Banner",
		Steps: []gnosis.Step{{
			Name: "Headline",
			Expect: &gnosis.Assertion{
				Timeout:    8 * time.Second,
				ScreenText: &gnosis.ScreenText{Text: "Sign in"},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsUnknownAttribute(t *testing.T) {
	path := filepath.Join("testdata", "attr-unknown.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   7,
		Column: 20,
		Reason: `unknown attribute "focused"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_attrEqualsMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "attr-equals.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Flags",
		Steps: []gnosis.Step{
			{
				Name: "Button is on",
				Expect: &gnosis.Assertion{
					Timeout: 4 * time.Second,
					AttrEquals: &gnosis.AttrEquals{
						Locator:   gnosis.Locator{ID: "send", Type: "Button"},
						Attribute: "enabled",
						Value:     true,
					},
				},
			},
			{
				Precondition: &gnosis.Assertion{
					Timeout: 10 * time.Second,
					AttrEquals: &gnosis.AttrEquals{
						Locator:   gnosis.Locator{Description: "Agree"},
						Attribute: "checked",
						Value:     false,
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_preconditionTextContainsMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "precondition-text-contains.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Hint",
		Steps: []gnosis.Step{{
			Precondition: &gnosis.Assertion{
				Timeout: 1500 * time.Millisecond,
				TextContains: &gnosis.TextCheck{
					Locator: gnosis.Locator{
						Description: "Status",
						Type:        "TextView",
					},
					Text: "saved",
				},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_expectTextEqualsMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "expect-text-equals.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Title",
		Steps: []gnosis.Step{{
			Name: "Heading",
			Expect: &gnosis.Assertion{
				Timeout: 3 * time.Second,
				TextEquals: &gnosis.TextCheck{
					Locator: gnosis.Locator{ID: "title", Index: 2},
					Text:    "Welcome",
				},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_preconditionAbsentMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "precondition-absent.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Gone",
		Steps: []gnosis.Step{{
			Name: "Dialog closed",
			Precondition: &gnosis.Assertion{
				Timeout: time.Second,
				Absent: &gnosis.Locator{
					ID:   "dialog",
					Text: "Saving",
				},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_expectExistsMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "expect-exists.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Signed in",
		Steps: []gnosis.Step{{
			Name: "Welcome is there",
			Expect: &gnosis.Assertion{
				Timeout: 2 * time.Second,
				Exists: &gnosis.Locator{
					Description: "Welcome",
					Type:        "TextView",
				},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_preconditionExistsMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "precondition-exists.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Ready",
		Steps: []gnosis.Step{{
			Name: "Login is there",
			Precondition: &gnosis.Assertion{
				Timeout: 5 * time.Second,
				Exists: &gnosis.Locator{
					ID:    "login",
					Text:  "Sign in",
					Index: 1,
				},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsAssertionTimeoutWithASpace(t *testing.T) {
	path := filepath.Join("testdata", "precondition-timeout-space.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 16,
		Reason: `invalid duration "10 s"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_writtenZeroAssertionTimeoutStaysZero(t *testing.T) {
	path := filepath.Join("testdata", "precondition-zero-timeout.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Steps[0].Precondition.Timeout != 0 {
		t.Fatalf("Load(%q) timeout = %s, want 0s", path, got.Steps[0].Precondition.Timeout)
	}
}

func TestLoad_omittedAssertionTimeoutIs10s(t *testing.T) {
	path := filepath.Join("testdata", "precondition-default-timeout.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Ready",
		Steps: []gnosis.Step{{
			Precondition: &gnosis.Assertion{
				Timeout: 10 * time.Second,
				Exists:  &gnosis.Locator{ID: "login"},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_caseWithOnlyAISteps(t *testing.T) {
	path := filepath.Join("testdata", "ai-only.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Ask",
		Steps: []gnosis.Step{
			{
				Name: "Settings",
				AI: &gnosis.AI{
					Goal:     "Open settings",
					MaxSteps: 20,
				},
			},
			{
				AI: &gnosis.AI{
					Goal:     "Turn on wifi",
					MaxSteps: 3,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_keepsAnEmptyAIGoal(t *testing.T) {
	path := filepath.Join("testdata", "ai-empty-goal.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := gnosis.Case{
		Name: "Ask",
		Steps: []gnosis.Step{{
			AI: &gnosis.AI{Goal: "", MaxSteps: 20},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_keepsMaxStepsOfOne(t *testing.T) {
	path := filepath.Join("testdata", "ai-max-steps-one.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Steps[0].AI.MaxSteps != 1 {
		t.Fatalf("Load(%q) max steps = %d, want 1", path, got.Steps[0].AI.MaxSteps)
	}
}

func TestLoad_rejectsAIWithoutGoal(t *testing.T) {
	path := filepath.Join("testdata", "ai-missing-goal.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing goal",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsTextEqualsWithoutText(t *testing.T) {
	path := filepath.Join("testdata", "text-equals-missing-text.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 7,
		Reason: "missing text",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsScreenTextWithALocator(t *testing.T) {
	path := filepath.Join("testdata", "screen-text-locator.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   6,
		Column: 9,
		Reason: `unknown field "locator"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsAssertionIndexBelowOne(t *testing.T) {
	path := filepath.Join("testdata", "exists-index-zero.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   5,
		Column: 16,
		Reason: "index must be >= 1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsAttributeValueThatIsNotABoolean(t *testing.T) {
	path := filepath.Join("testdata", "attr-value-string.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   8,
		Column: 16,
		Reason: `field "value" must be a boolean`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_doesNotModifyAssertionOrAIFiles(t *testing.T) {
	paths := []string{
		filepath.Join("testdata", "assertions-and-ai.yaml"),
		filepath.Join("testdata", "ai-only.yaml"),
		filepath.Join("testdata", "unknown-check.yaml"),
		filepath.Join("testdata", "assertion-missing-position.yaml"),
		filepath.Join("testdata", "attr-unknown.yaml"),
		filepath.Join("testdata", "ai-max-steps-zero.yaml"),
		filepath.Join("testdata", "precondition-timeout-space.yaml"),
	}
	for _, path := range paths {
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		_, _, _ = gnosis.Load(path)
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("Load(%q) modified the file", path)
		}
	}
}
