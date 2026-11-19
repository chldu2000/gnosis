package gnosis

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"sync"
	"time"
)

// Device provides the operations available to a loaded case.
// TryOccupy must acquire ownership atomically. Release relinquishes that ownership.
type Device interface {
	ID() string
	Online() bool
	Occupied() bool
	TryOccupy() bool
	Release()
	EnsureAutomation() error
	ScreenSize() (image.Point, error)
	ControlTree() ([]Element, error)
	Screenshot() ([]byte, error)
	RecognizeScreenText([]byte) ([]TextRegion, error)
	Tap(image.Point) error
	LongPress(image.Point, time.Duration) error
	Swipe(from, to image.Point, duration time.Duration) error
	// ReplaceText reports whether it changed the input method, even on error.
	ReplaceText(string) (changedIME bool, err error)
	PressKey(string) error
	Launch(app, entry string) error
	RestoreIME() error
}

// Element is a node in the control tree, including nodes outside the screen.
// A nil attribute means the device did not report it.
type Element struct {
	ID, Text, Description, Type string
	Bounds                      image.Rectangle
	Enabled, Checked            *bool
}

// TextRegion is a string recognized from the pixels of a screenshot.
type TextRegion struct {
	Text   string
	Bounds image.Rectangle
}

type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

// RealClock uses wall time for device executions.
type RealClock struct{}

func (RealClock) Now() time.Time        { return time.Now() }
func (RealClock) Sleep(d time.Duration) { time.Sleep(d) }

type Result string

const (
	ResultPass     Result = "pass"
	ResultFail     Result = "fail"
	ResultBlocked  Result = "blocked"
	ResultError    Result = "error"
	ResultExecuted Result = "executed"
)

type RunRecord struct {
	CaseName  string
	DeviceID  string
	StartedAt time.Time
	Result    Result
	Steps     []StepResult
	IMENote   string
	Error     error
}

type StepResult struct {
	Name       string
	Result     Result
	Screenshot []byte
	Trajectory []TrajectoryTurn
	Error      error
}

// TrajectoryTurn records one dispatched model action and its resulting frame.
// Action is an ordinary action step. Screenshot is nil if capture failed.
type TrajectoryTurn struct {
	Action     Step
	Note       string
	Screenshot []byte
}

var (
	ErrDeviceOffline  = errors.New("device is offline")
	ErrDeviceOccupied = errors.New("device is occupied")
	ErrRunInProgress  = errors.New("a run is already in progress in this process")
	runLock           sync.Mutex
)

// Run executes a loaded case. A refusal returns no record. Once started,
// operation errors are recorded in the run and its step results. Supply model
// settings with WithModel when the case needs to execute an AI step.
func Run(testCase Case, device Device, clock Clock, options ...RunOption) (*RunRecord, error) {
	if !runLock.TryLock() {
		return nil, ErrRunInProgress
	}
	defer runLock.Unlock()
	if !device.Online() {
		return nil, ErrDeviceOffline
	}
	if device.Occupied() || !device.TryOccupy() {
		return nil, ErrDeviceOccupied
	}
	defer device.Release()

	var settings runSettings
	for _, option := range options {
		option(&settings)
	}
	runner := caseRun{device: device, clock: clock, settings: settings}
	record := &RunRecord{
		CaseName: testCase.Name, DeviceID: device.ID(), StartedAt: clock.Now(),
		Result: ResultExecuted,
	}
	if err := device.EnsureAutomation(); err != nil {
		record.Result = ResultError
		record.Error = safeRunError(fmt.Errorf("prepare device automation: %w", err), settings.model.APIKey)
		return record, nil
	}

	for _, step := range testCase.Steps {
		conclusion := runner.step(step)
		record.Steps = append(record.Steps, conclusion)
		switch conclusion.Result {
		case ResultPass:
			if step.Expect != nil {
				record.Result = ResultPass
			}
		case ResultFail, ResultBlocked, ResultError:
			record.Result = conclusion.Result
			record.Error = conclusion.Error
		}
		if record.Result == ResultFail || record.Result == ResultBlocked || record.Result == ResultError {
			break
		}
	}
	if runner.changedIME {
		if err := device.RestoreIME(); err != nil {
			record.IMENote = safeRunError(fmt.Errorf("restore input method: %w", err), settings.model.APIKey).Error()
		}
	}
	return record, nil
}

type caseRun struct {
	device     Device
	clock      Clock
	settings   runSettings
	changedIME bool
}

// frame caches a screenshot during a single observation. A pixel assertion
// saves the same screenshot it evaluated when that observation ends the step.
type frame struct {
	device Device
	taken  bool
	data   []byte
	err    error
}

func (f *frame) screenshot() ([]byte, error) {
	if !f.taken {
		f.taken = true
		data, err := f.device.Screenshot()
		if err == nil && len(data) == 0 {
			err = errors.New("device returned an empty screenshot")
		}
		if err != nil {
			f.err = fmt.Errorf("take screenshot: %w", err)
		} else {
			f.data = bytes.Clone(data)
		}
	}
	return f.data, f.err
}

func (f *frame) screenText() ([]TextRegion, error) {
	data, err := f.screenshot()
	if err != nil {
		return nil, err
	}
	return f.device.RecognizeScreenText(data)
}

func (r *caseRun) step(step Step) StepResult {
	end := &frame{device: r.device}
	conclusion := StepResult{Name: step.Name, Result: ResultExecuted}
	var err error
	switch {
	case step.Precondition != nil:
		conclusion.Result, end, err = r.assert(step.Precondition, ResultBlocked)
	case step.Expect != nil:
		conclusion.Result, end, err = r.assert(step.Expect, ResultFail)
	case step.AI != nil:
		conclusion.Trajectory, end, err = r.runAI(*step.AI, end)
	default:
		var dispatched bool
		dispatched, err = r.action(step, end)
		if dispatched {
			end = &frame{device: r.device}
		}
	}

	screenshot, screenshotErr := end.screenshot()
	if err == nil {
		err = screenshotErr
	}
	if err != nil {
		conclusion.Result = ResultError
		conclusion.Error = safeRunError(err, r.settings.model.APIKey)
	}
	conclusion.Screenshot = screenshot
	return conclusion
}

// action reports whether it dispatched the action, even if the device failed
// while carrying it out. Looking up a target does not dispatch the action.
func (r *caseRun) action(step Step, observation *frame) (bool, error) {
	switch {
	case step.Launch != nil:
		return true, r.device.Launch(step.Launch.App, step.Launch.Entry)
	case step.TapAt != nil:
		points, err := r.screenPoints([2]float64{step.TapAt.X, step.TapAt.Y})
		if err != nil {
			return false, err
		}
		return true, r.device.Tap(points[0])
	case step.Swipe != nil:
		s := step.Swipe
		points, err := r.screenPoints([2]float64{s.FromX, s.FromY}, [2]float64{s.ToX, s.ToY})
		if err != nil {
			return false, err
		}
		return true, r.device.Swipe(points[0], points[1], s.Duration)
	case step.LongPress != nil:
		point, err := locateOne(step.LongPress.Locator, observation)
		if err != nil {
			return false, err
		}
		return true, r.device.LongPress(point, step.LongPress.Duration)
	case step.Type != nil:
		changed, err := r.device.ReplaceText(step.Type.Text)
		r.changedIME = r.changedIME || changed
		return true, err
	case step.Key != nil:
		if step.Key.Name != "back" && step.Key.Name != "home" {
			return false, fmt.Errorf("unsupported key %q", step.Key.Name)
		}
		return true, r.device.PressKey(step.Key.Name)
	case step.Wait != nil:
		if step.Wait.Duration > 0 {
			r.clock.Sleep(step.Wait.Duration)
		}
		return true, nil
	case step.Locator != (Locator{}):
		point, err := locateOne(step.Locator, observation)
		if err != nil {
			return false, err
		}
		return true, r.device.Tap(point)
	default:
		return false, errors.New("step has no action")
	}
}

func (r *caseRun) screenPoints(ratios ...[2]float64) ([]image.Point, error) {
	for _, ratio := range ratios {
		for _, v := range ratio {
			if !(v >= 0 && v <= 1) {
				return nil, fmt.Errorf("screen ratio %g is outside [0, 1]", v)
			}
		}
	}
	size, err := r.device.ScreenSize()
	if err != nil {
		return nil, err
	}
	if size.X <= 0 || size.Y <= 0 {
		return nil, fmt.Errorf("invalid screen size %v", size)
	}
	points := make([]image.Point, 0, len(ratios))
	for _, ratio := range ratios {
		points = append(points, image.Pt(
			int(math.Round(ratio[0]*float64(size.X-1))),
			int(math.Round(ratio[1]*float64(size.Y-1))),
		))
	}
	return points, nil
}
