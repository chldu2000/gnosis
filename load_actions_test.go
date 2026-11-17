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

func TestLoad_launchAppAndEntryMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "launch.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Launch mail",
		Steps: []gnosis.Step{{
			Name: "Open mail",
			Launch: &gnosis.Launch{
				App:   "com.example.mail",
				Entry: ".Inbox",
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsLaunchWithoutApp(t *testing.T) {
	path := filepath.Join("testdata", "launch-missing-app.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing app",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_omittedLaunchEntryStaysEmpty(t *testing.T) {
	path := filepath.Join("testdata", "launch-omitted-entry.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Launch mail",
		Steps: []gnosis.Step{{
			Launch: &gnosis.Launch{App: "com.example.mail"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsLaunchAppThatIsNotAString(t *testing.T) {
	path := filepath.Join("testdata", "launch-app-wrong-type.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 12,
		Reason: `field "app" must be a string`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsUnknownFieldOnLaunch(t *testing.T) {
	path := filepath.Join("testdata", "launch-unknown-field.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   5,
		Column: 7,
		Reason: `unknown field "bundle"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsLaunchThatIsNotAMapping(t *testing.T) {
	path := filepath.Join("testdata", "launch-not-a-mapping.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 13,
		Reason: "launch must be a mapping",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_keepsAnEmptyLaunchApp(t *testing.T) {
	path := filepath.Join("testdata", "launch-empty-app.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := gnosis.Case{
		Name: "Launch mail",
		Steps: []gnosis.Step{{
			Launch: &gnosis.Launch{App: ""},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsStepWithTwoActions(t *testing.T) {
	path := filepath.Join("testdata", "two-actions.yaml")

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

func TestLoad_longPressLocatorAndDurationMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "long-press.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Press",
		Steps: []gnosis.Step{
			{
				Name: "Hold send",
				LongPress: &gnosis.LongPress{
					Locator: gnosis.Locator{
						ID:    locatorString("send"),
						Text:  locatorString("Send"),
						Index: 1,
					},
					Duration: 2 * time.Second,
				},
			},
			{
				LongPress: &gnosis.LongPress{
					Locator: gnosis.Locator{
						Description: locatorString("Hold"),
						Type:        locatorString("Button"),
					},
					Duration: 1500 * time.Millisecond,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_omittedLongPressDurationIs800ms(t *testing.T) {
	path := filepath.Join("testdata", "long-press-default.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Press",
		Steps: []gnosis.Step{{
			LongPress: &gnosis.LongPress{
				Locator:  gnosis.Locator{ID: locatorString("send")},
				Duration: 800 * time.Millisecond,
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_writtenZeroLongPressDurationStaysZero(t *testing.T) {
	path := filepath.Join("testdata", "long-press-zero.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Steps[0].LongPress.Duration != 0 {
		t.Fatalf("Load(%q) duration = %s, want 0s", path, got.Steps[0].LongPress.Duration)
	}
}

func TestLoad_rejectsBadLongPressDurations(t *testing.T) {
	tests := []struct {
		file   string
		reason string
	}{
		{file: "long-press-duration-space.yaml", reason: `invalid duration "800 ms"`},
		{file: "long-press-duration-decimal.yaml", reason: `invalid duration "1.5s"`},
		{file: "long-press-duration-unit.yaml", reason: `invalid duration "800"`},
		{file: "long-press-duration-negative.yaml", reason: `invalid duration "-1s"`},
		{file: "long-press-duration-case.yaml", reason: `invalid duration "2S"`},
		{file: "long-press-duration-overflow.yaml", reason: `invalid duration "99999999999999999999s"`},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			path := filepath.Join("testdata", test.file)

			_, _, err := gnosis.Load(path)

			var got *gnosis.Rejection
			if !errors.As(err, &got) {
				t.Fatalf("Load(%q) err = %v, want rejection", path, err)
			}
			want := &gnosis.Rejection{
				File:   path,
				Line:   6,
				Column: 17,
				Reason: test.reason,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
			}
		})
	}
}

func TestLoad_rejectsLongPressWithoutLocator(t *testing.T) {
	path := filepath.Join("testdata", "long-press-missing-locator.yaml")

	_, _, err := gnosis.Load(path)

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

func TestLoad_rejectsLongPressIndexBelowOne(t *testing.T) {
	path := filepath.Join("testdata", "long-press-index-zero.yaml")

	_, _, err := gnosis.Load(path)

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

func TestLoad_tapAtRatiosMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "tap-at.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Point",
		Steps: []gnosis.Step{
			{
				Name:  "Top",
				TapAt: &gnosis.TapAt{X: 0.25, Y: 0.75},
			},
			{TapAt: &gnosis.TapAt{X: -0.25, Y: 1.5}},
			{TapAt: &gnosis.TapAt{X: 2, Y: -1}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_swipeRatiosAndDurationMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "swipe.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Drag",
		Steps: []gnosis.Step{
			{
				Name: "Across",
				Swipe: &gnosis.Swipe{
					FromX:    -0.25,
					FromY:    0,
					ToX:      1.5,
					ToY:      2,
					Duration: 2 * time.Second,
				},
			},
			{
				Swipe: &gnosis.Swipe{
					FromX:    0.25,
					FromY:    0.75,
					ToX:      0.75,
					ToY:      0.25,
					Duration: 1500 * time.Millisecond,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_writtenZeroSwipeDurationStaysZero(t *testing.T) {
	path := filepath.Join("testdata", "swipe-zero.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Steps[0].Swipe.Duration != 0 {
		t.Fatalf("Load(%q) duration = %s, want 0s", path, got.Steps[0].Swipe.Duration)
	}
}

func TestLoad_omittedSwipeDurationIs300ms(t *testing.T) {
	path := filepath.Join("testdata", "swipe-default.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Drag",
		Steps: []gnosis.Step{{
			Swipe: &gnosis.Swipe{
				FromX:    0,
				FromY:    1,
				ToX:      1,
				ToY:      0,
				Duration: 300 * time.Millisecond,
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_typeTextMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "type.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Enter",
		Steps: []gnosis.Step{
			{
				Name: "Username",
				Type: &gnosis.Type{Text: "Ada"},
			},
			{Type: &gnosis.Type{Text: ""}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_keyNamesMatchTheFile(t *testing.T) {
	path := filepath.Join("testdata", "key.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Keys",
		Steps: []gnosis.Step{
			{
				Name: "Leave",
				Key:  &gnosis.Key{Name: "back"},
			},
			{Key: &gnosis.Key{Name: "home"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsUnknownKeyName(t *testing.T) {
	path := filepath.Join("testdata", "key-unknown.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 13,
		Reason: `unknown key "menu"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsKeyWithoutName(t *testing.T) {
	path := filepath.Join("testdata", "key-missing-name.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing name",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_waitDurationMatchesTheFile(t *testing.T) {
	path := filepath.Join("testdata", "wait.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "Pause",
		Steps: []gnosis.Step{
			{
				Name: "Settle",
				Wait: &gnosis.Wait{Duration: 10 * time.Second},
			},
			{Wait: &gnosis.Wait{Duration: 0}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsWaitWithoutDuration(t *testing.T) {
	path := filepath.Join("testdata", "wait-missing-duration.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing duration",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsBadWaitDuration(t *testing.T) {
	path := filepath.Join("testdata", "wait-duration-decimal.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 17,
		Reason: `invalid duration "1.5s"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsUnknownActionKindAtItsPosition(t *testing.T) {
	path := filepath.Join("testdata", "unknown-action.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: `unknown field "drag"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_actionsKeepFileOrderDefaultsAndOutOfRangeRatios(t *testing.T) {
	path := filepath.Join("testdata", "actions.yaml")

	got, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := gnosis.Case{
		Name: "All actions",
		Steps: []gnosis.Step{
			{
				Name:   "Open",
				Launch: &gnosis.Launch{App: "com.example.mail"},
			},
			{Locator: gnosis.Locator{ID: locatorString("login")}},
			{
				LongPress: &gnosis.LongPress{
					Locator:  gnosis.Locator{Text: locatorString("Send")},
					Duration: 800 * time.Millisecond,
				},
			},
			{TapAt: &gnosis.TapAt{X: -0.5, Y: 1.25}},
			{
				Swipe: &gnosis.Swipe{
					FromX:    0,
					FromY:    -1,
					ToX:      2,
					ToY:      0.5,
					Duration: 300 * time.Millisecond,
				},
			},
			{Type: &gnosis.Type{Text: "hello"}},
			{Key: &gnosis.Key{Name: "back"}},
			{Wait: &gnosis.Wait{Duration: time.Second}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) = %+v, want %+v", path, got, want)
	}
}

func TestLoad_doesNotModifyActionFiles(t *testing.T) {
	paths := []string{
		filepath.Join("testdata", "actions.yaml"),
		filepath.Join("testdata", "long-press-default.yaml"),
		filepath.Join("testdata", "swipe-default.yaml"),
		filepath.Join("testdata", "key-unknown.yaml"),
		filepath.Join("testdata", "wait-missing-duration.yaml"),
		filepath.Join("testdata", "long-press-duration-space.yaml"),
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

func TestLoad_rejectsTapAtWithoutY(t *testing.T) {
	path := filepath.Join("testdata", "tap-at-missing-y.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing y",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsTapAtXThatIsNotANumber(t *testing.T) {
	path := filepath.Join("testdata", "tap-at-x-string.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   4,
		Column: 10,
		Reason: `field "x" must be a number`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsSwipeWithoutToY(t *testing.T) {
	path := filepath.Join("testdata", "swipe-missing-to-y.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing to_y",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsSwipeDurationWithASpace(t *testing.T) {
	path := filepath.Join("testdata", "swipe-duration-space.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   8,
		Column: 17,
		Reason: `invalid duration "300 ms"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}

func TestLoad_rejectsTypeWithoutText(t *testing.T) {
	path := filepath.Join("testdata", "type-missing-text.yaml")

	_, _, err := gnosis.Load(path)

	var got *gnosis.Rejection
	if !errors.As(err, &got) {
		t.Fatalf("Load(%q) err = %v, want rejection", path, err)
	}
	want := &gnosis.Rejection{
		File:   path,
		Line:   3,
		Column: 5,
		Reason: "missing text",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(%q) err = %+v, want %+v", path, got, want)
	}
}
