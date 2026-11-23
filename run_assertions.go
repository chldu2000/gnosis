package gnosis

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strings"
	"time"
)

var errRelativeScopeMissing = errors.New("relative locator anchor or container was not found")

func (r *caseRun) assert(assertion *Assertion, onFalse Result) (Result, *frame, error) {
	control := r.settings.control
	deadline := r.clock.Now().Add(assertion.Timeout - control.pauseTime())
	for {
		observation := &frame{device: r.device}
		if err := control.checkpoint(); err != nil {
			return ResultError, observation, err
		}
		pausedBefore := control.pauseGeneration()
		ok, err := checkAssertion(assertion, observation)
		if errors.Is(err, errRelativeScopeMissing) {
			// Even an absence assertion needs its reference to establish the scope.
			ok, err = false, nil
		}
		if checkpointErr := control.checkpoint(); checkpointErr != nil {
			return ResultError, observation, checkpointErr
		}
		if pausedBefore != control.pauseGeneration() && err == nil {
			continue
		}
		switch {
		case err != nil:
			return ResultError, observation, err
		case ok:
			return ResultPass, observation, nil
		}
		remaining := deadline.Sub(r.clock.Now().Add(-control.pauseTime()))
		if remaining <= 0 {
			return onFalse, observation, nil
		}
		if err := r.wait(min(remaining, 500*time.Millisecond)); err != nil {
			return ResultError, observation, err
		}
	}
}

func checkAssertion(assertion *Assertion, observation *frame) (bool, error) {
	switch {
	case assertion.ScreenText != nil:
		regions, err := observation.screenText()
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(regions, func(region TextRegion) bool {
			return strings.Contains(region.Text, assertion.ScreenText.Text)
		}), nil
	case assertion.Exists != nil:
		matches, err := locate(*assertion.Exists, observation, true)
		return len(matches) > 0, err
	case assertion.Absent != nil:
		matches, err := locate(*assertion.Absent, observation, true)
		return len(matches) == 0, err
	case assertion.TextEquals != nil:
		check := assertion.TextEquals
		return anyTreeMatch(check.Locator, observation, func(e Element) bool {
			return e.Text == check.Text
		})
	case assertion.TextContains != nil:
		check := assertion.TextContains
		return anyTreeMatch(check.Locator, observation, func(e Element) bool {
			return strings.Contains(e.Text, check.Text)
		})
	case assertion.AttrEquals != nil:
		check := assertion.AttrEquals
		var attribute func(Element) *bool
		switch check.Attribute {
		case "enabled":
			attribute = func(e Element) *bool { return e.Enabled }
		case "checked":
			attribute = func(e Element) *bool { return e.Checked }
		default:
			return false, fmt.Errorf("unsupported element attribute %q", check.Attribute)
		}
		return anyTreeMatch(check.Locator, observation, func(e Element) bool {
			value := attribute(e)
			return value != nil && *value == check.Value
		})
	default:
		return false, errors.New("assertion has no check")
	}
}

func anyTreeMatch(locator Locator, observation *frame, predicate func(Element) bool) (bool, error) {
	matches, err := locate(locator, observation, false)
	return slices.ContainsFunc(matches, predicate), err
}

func locateOne(locator Locator, observation *frame) (image.Point, error) {
	matches, err := locate(locator, observation, true)
	if err != nil {
		return image.Point{}, err
	}
	if len(matches) != 1 {
		return image.Point{}, fmt.Errorf("expected one matching element, found %d", len(matches))
	}
	bounds := matches[0].Bounds
	if bounds.Empty() {
		return image.Point{}, errors.New("matched element has no bounds")
	}
	return image.Pt(
		int(math.Round((float64(bounds.Min.X)+float64(bounds.Max.X))/2)),
		int(math.Round((float64(bounds.Min.Y)+float64(bounds.Max.Y))/2)),
	), nil
}

func locate(locator Locator, observation *frame, allowScreenText bool) ([]Element, error) {
	tree, err := observation.device.ControlTree()
	if err != nil {
		return nil, fmt.Errorf("read control tree: %w", err)
	}
	var matches []Element
	if locator.Relative != nil {
		preview := (&InterfaceCapture{Elements: tree}).PreviewRelative(locator)
		if len(preview.Anchors) > 1 {
			return nil, fmt.Errorf("relative locator expected one anchor, found %d", len(preview.Anchors))
		}
		if len(preview.Anchors) == 0 || (locator.Relative.Relation == "within" && preview.Container < 0) {
			return nil, errRelativeScopeMissing
		}
		for _, i := range preview.Targets {
			matches = append(matches, tree[i])
		}
	} else {
		matches = matchTree(locator, tree)
	}
	if locator.Relative == nil && len(matches) == 0 && allowScreenText && locator.Text != nil &&
		locator.ID == nil && locator.Description == nil && locator.Type == nil {
		regions, err := observation.screenText()
		if err != nil {
			return nil, err
		}
		for _, region := range regions {
			if region.Text == *locator.Text {
				matches = append(matches, Element{Bounds: region.Bounds})
			}
		}
		sortScreenMatches(matches)
	}
	if locator.Index == 0 {
		return matches, nil
	}
	if locator.Index < 1 || locator.Index > len(matches) {
		return nil, nil
	}
	return matches[locator.Index-1 : locator.Index], nil
}

func matchesField(condition *string, value string) bool {
	return condition == nil || *condition == value
}

func sortScreenMatches(matches []Element) {
	slices.SortStableFunc(matches, func(a, b Element) int {
		return cmp.Compare(a.Bounds.Min.Y, b.Bounds.Min.Y)
	})
	for first := 0; first < len(matches); {
		bounds := matches[first].Bounds
		// Different glyphs on one line can have slightly different top edges.
		// Use the first box's vertical midpoint to keep those boxes together.
		midpoint := bounds.Min.Y + bounds.Dy()/2
		last := first + 1
		for last < len(matches) && matches[last].Bounds.Min.Y <= midpoint {
			last++
		}
		slices.SortStableFunc(matches[first:last], func(a, b Element) int {
			return cmp.Compare(a.Bounds.Min.X, b.Bounds.Min.X)
		})
		first = last
	}
}
