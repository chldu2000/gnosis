package gnosis_test

import (
	"errors"
	"fmt"
	"image"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"gnosis"
)

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time        { return c.now }
func (c *testClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

type testDevice struct {
	offline, occupied, lostClaim bool
	installed, changeIME         bool
	calls                        []string
	fail                         string
	fault                        error
	size                         image.Point
	trees                        [][]gnosis.Element
	regions                      [][]gnosis.TextRegion
	reads, frames, recognitions  int
	onPrepare                    func()
}

func (d *testDevice) ID() string     { return "android-01" }
func (d *testDevice) Online() bool   { return !d.offline }
func (d *testDevice) Occupied() bool { return d.occupied }
func (d *testDevice) TryOccupy() bool {
	d.call("occupy")
	if d.lostClaim {
		return false
	}
	d.occupied = true
	return true
}
func (d *testDevice) Release() {
	d.call("release")
	d.occupied = false
}
func (d *testDevice) EnsureAutomation() error {
	if d.onPrepare != nil {
		d.onPrepare()
	}
	if d.installed {
		return nil
	}
	if err := d.call("install"); err != nil {
		return err
	}
	d.installed = true
	return nil
}
func (d *testDevice) ScreenSize() (image.Point, error) {
	return d.size, d.call("size")
}
func (d *testDevice) ControlTree() ([]gnosis.Element, error) {
	d.reads++
	var elements []gnosis.Element
	if len(d.trees) > 0 {
		elements = d.trees[min(d.reads-1, len(d.trees)-1)]
	}
	return elements, d.call(fmt.Sprint("tree ", d.reads))
}
func (d *testDevice) Screenshot() ([]byte, error) {
	d.frames++
	return []byte{byte(d.frames)}, d.call("screenshot")
}
func (d *testDevice) RecognizeScreenText(data []byte) ([]gnosis.TextRegion, error) {
	d.recognitions++
	if d.frames != int(data[0]) {
		return nil, errors.New("wrong OCR frame")
	}
	var regions []gnosis.TextRegion
	if len(d.regions) > 0 {
		regions = d.regions[min(d.recognitions-1, len(d.regions)-1)]
	}
	return regions, d.call("ocr")
}
func (d *testDevice) Tap(p image.Point) error {
	return d.call(fmt.Sprint("tap ", p))
}
func (d *testDevice) LongPress(p image.Point, dur time.Duration) error {
	return d.call(fmt.Sprintf("hold %v %v", p, dur))
}
func (d *testDevice) Swipe(from, to image.Point, dur time.Duration) error {
	return d.call(fmt.Sprintf("swipe %v %v %v", from, to, dur))
}
func (d *testDevice) ReplaceText(s string) (bool, error) {
	return d.changeIME, d.call("text " + s)
}
func (d *testDevice) PressKey(key string) error { return d.call(key) }
func (d *testDevice) Launch(app, entry string) error {
	return d.call(fmt.Sprintf("launch %s %s", app, entry))
}
func (d *testDevice) RestoreIME() error { return d.call("restore") }
func (d *testDevice) call(name string) error {
	d.calls = append(d.calls, name)
	if name == d.fail {
		return d.fault
	}
	return nil
}

func runCase(t *testing.T, device *testDevice, clock *testClock, steps ...gnosis.Step) *gnosis.RunRecord {
	t.Helper()
	record, err := gnosis.Run(gnosis.Case{Name: "Example", Steps: steps}, device, clock)
	if err != nil || record == nil {
		t.Fatalf("Run = %v, %v", record, err)
	}
	return record
}

func TestRunRefusesWithoutClaimingOrChangingIME(t *testing.T) {
	for _, test := range []struct {
		name string
		dev  *testDevice
		err  error
	}{
		{"offline", &testDevice{offline: true}, gnosis.ErrDeviceOffline},
		{"occupied", &testDevice{occupied: true}, gnosis.ErrDeviceOccupied},
		{"claim lost", &testDevice{lostClaim: true}, gnosis.ErrDeviceOccupied},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, err := gnosis.Run(gnosis.Case{}, test.dev, &testClock{})
			if record != nil || !errors.Is(err, test.err) {
				t.Fatalf("Run = %v, %v", record, err)
			}
			if test.dev.lostClaim {
				if !slices.Equal(test.dev.calls, []string{"occupy"}) {
					t.Fatalf("operations = %v", test.dev.calls)
				}
			} else if len(test.dev.calls) > 0 {
				t.Fatalf("unexpected operations = %v", test.dev.calls)
			}
		})
	}
	d := &testDevice{onPrepare: func() {
		second := &testDevice{}
		record, err := gnosis.Run(gnosis.Case{}, second, &testClock{})
		if record != nil || !errors.Is(err, gnosis.ErrRunInProgress) || len(second.calls) != 0 {
			t.Errorf("concurrent Run = %v, %v, %v", record, err, second.calls)
		}
	}}
	clock := &testClock{now: time.Unix(7, 0)}
	record := runCase(t, d, clock)
	if record.CaseName != "Example" || record.DeviceID != d.ID() || record.StartedAt != clock.now ||
		record.Result != gnosis.ResultExecuted || len(record.Steps) != 0 || d.occupied {
		t.Fatalf("empty run = %+v, occupied %v", record, d.occupied)
	}
}

func TestRunActionsAndCleanupInOrder(t *testing.T) {
	d := &testDevice{size: image.Pt(8, 6), changeIME: true, fail: "restore", fault: errors.New("restore failed")}
	clock := &testClock{now: time.Unix(12, 0)}
	run := runCase(t, d, clock,
		gnosis.Step{Name: "Open", Launch: &gnosis.Launch{App: "example"}},
		gnosis.Step{Launch: &gnosis.Launch{App: "example", Entry: ".Main"}},
		gnosis.Step{TapAt: &gnosis.TapAt{X: 0, Y: 1}},
		gnosis.Step{Swipe: &gnosis.Swipe{FromX: 1, FromY: 0, ToX: 0.5, ToY: 0.5, Duration: 810 * time.Millisecond}},
		gnosis.Step{Type: &gnosis.Type{Text: "中文"}},
		gnosis.Step{Key: &gnosis.Key{Name: "back"}},
		gnosis.Step{Key: &gnosis.Key{Name: "home"}},
		gnosis.Step{Wait: &gnosis.Wait{Duration: 35 * time.Millisecond}},
		gnosis.Step{Wait: &gnosis.Wait{}},
	)
	want := []string{"occupy", "install", "launch example ", "screenshot", "launch example .Main", "screenshot",
		"size", "tap (0,5)", "screenshot", "size", "swipe (7,0) (4,3) 810ms", "screenshot",
		"text 中文", "screenshot", "back", "screenshot", "home", "screenshot", "screenshot", "screenshot", "restore", "release"}
	if !slices.Equal(d.calls, want) {
		t.Fatalf("operations = %v, want %v", d.calls, want)
	}
	if run.Result != gnosis.ResultExecuted || run.IMENote == "" || clock.now.Sub(run.StartedAt) != 35*time.Millisecond {
		t.Fatalf("run = %+v, elapsed = %v", run, clock.now.Sub(run.StartedAt))
	}
	for i, step := range run.Steps {
		name := ""
		if i == 0 {
			name = "Open"
		}
		if step.Name != name || step.Result != gnosis.ResultExecuted || !slices.Equal(step.Screenshot, []byte{byte(i + 1)}) {
			t.Fatalf("step %d = %+v", i, step)
		}
	}
	d.calls = nil
	runCase(t, d, clock)
	if !slices.Equal(d.calls, []string{"occupy", "release"}) {
		t.Fatalf("repeat operations = %v", d.calls)
	}
}

func TestRunStopsOnOperationErrors(t *testing.T) {
	for _, test := range []struct {
		name, fail string
		step       gnosis.Step
		operations []string
	}{
		{"install", "install", gnosis.Step{Wait: &gnosis.Wait{}}, []string{"occupy", "install", "release"}},
		{"launch", "launch bad ", gnosis.Step{Launch: &gnosis.Launch{App: "bad"}}, []string{"occupy", "install", "launch bad ", "screenshot", "release"}},
		{"input after switch", "text replacement", gnosis.Step{Type: &gnosis.Type{Text: "replacement"}}, []string{"occupy", "install", "text replacement", "screenshot", "restore", "release"}},
		{"disconnect", "back", gnosis.Step{Key: &gnosis.Key{Name: "back"}}, []string{"occupy", "install", "back", "screenshot", "release"}},
		{"screenshot", "screenshot", gnosis.Step{Wait: &gnosis.Wait{}}, []string{"occupy", "install", "screenshot", "release"}},
		{"size", "size", gnosis.Step{TapAt: &gnosis.TapAt{}}, []string{"occupy", "install", "size", "screenshot", "release"}},
		{"out of bounds", "", gnosis.Step{Swipe: &gnosis.Swipe{ToX: 1.1}}, []string{"occupy", "install", "screenshot", "release"}},
		{"non-number", "", gnosis.Step{TapAt: &gnosis.TapAt{Y: math.NaN()}}, []string{"occupy", "install", "screenshot", "release"}},
		{"AI", "", gnosis.Step{AI: &gnosis.AI{Goal: "Open inbox"}}, []string{"occupy", "install", "screenshot", "release"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &testDevice{changeIME: true, fail: test.fail, fault: errors.New("operation failed"), size: image.Pt(8, 6)}
			run := runCase(t, d, &testClock{}, test.step, gnosis.Step{Key: &gnosis.Key{Name: "home"}})
			wantSteps := 1
			if test.fail == "install" {
				wantSteps = 0
			}
			if run.Result != gnosis.ResultError || run.Error == nil || len(run.Steps) != wantSteps ||
				d.occupied || !slices.Equal(d.calls, test.operations) {
				t.Fatalf("run = %+v, operations = %v", run, d.calls)
			}
		})
	}
}

func TestRunTreePriorityAndScreenTextOrder(t *testing.T) {
	text := "打开"
	bounds := image.Rect(2, 3, 7, 10)
	regions := []gnosis.TextRegion{
		{Text: text, Bounds: image.Rect(7, 12, 17, 18)},
		{Text: text, Bounds: image.Rect(25, 4, 35, 10)},
		{Text: text, Bounds: image.Rect(5, 5, 15, 11)},
	}
	d := &testDevice{trees: [][]gnosis.Element{{{Text: text, Bounds: bounds}}, nil}, regions: [][]gnosis.TextRegion{regions}}
	run := runCase(t, d, &testClock{},
		gnosis.Step{Locator: gnosis.Locator{Text: &text}},
		gnosis.Step{LongPress: &gnosis.LongPress{Locator: gnosis.Locator{Text: &text, Index: 2}, Duration: 800 * time.Millisecond}},
	)
	if run.Result != gnosis.ResultExecuted || !slices.Contains(d.calls, "tap (5,7)") ||
		!slices.Contains(d.calls, "hold (30,7) 800ms") || d.recognitions != 1 ||
		!slices.Equal(run.Steps[1].Screenshot, []byte{3}) {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
	for _, test := range []struct {
		name string
		loc  gnosis.Locator
		tree []gnosis.Element
	}{
		{"explicit empty ID", gnosis.Locator{ID: locatorString(""), Text: &text}, nil},
		{"ambiguous", gnosis.Locator{Text: &text}, []gnosis.Element{{Text: text}, {Text: text}}},
		{"index too large", gnosis.Locator{Text: &text, Index: 2}, []gnosis.Element{{Text: text}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &testDevice{trees: [][]gnosis.Element{test.tree}, regions: [][]gnosis.TextRegion{regions}}
			run := runCase(t, d, &testClock{}, gnosis.Step{Locator: test.loc})
			if run.Result != gnosis.ResultError || d.recognitions != 0 || !slices.Equal(d.calls, []string{"occupy", "install", "tree 1", "screenshot", "release"}) {
				t.Fatalf("run = %+v, operations = %v", run, d.calls)
			}
		})
	}
}

func TestRunAssertionResultsAndPolling(t *testing.T) {
	id, txt, on := "item", "hello, world", true
	target := gnosis.Locator{ID: &id}
	tree := []gnosis.Element{{ID: id}, {ID: id, Text: txt, Checked: &on, Bounds: image.Rect(-200, -150, -100, -50)}}
	clock := &testClock{}
	d := &testDevice{trees: [][]gnosis.Element{nil, tree}}
	run := runCase(t, d, clock,
		gnosis.Step{Precondition: &gnosis.Assertion{Exists: &target, Timeout: time.Second}},
		gnosis.Step{Expect: &gnosis.Assertion{TextEquals: &gnosis.TextCheck{Locator: target, Text: txt}}},
		gnosis.Step{Expect: &gnosis.Assertion{TextContains: &gnosis.TextCheck{Locator: target, Text: "world"}}},
		gnosis.Step{Expect: &gnosis.Assertion{AttrEquals: &gnosis.AttrEquals{Locator: target, Attribute: "checked", Value: true}}},
		gnosis.Step{Expect: &gnosis.Assertion{Absent: &gnosis.Locator{ID: &id, Index: 3}}},
	)
	if run.Result != gnosis.ResultPass || len(run.Steps) != 5 || d.reads != 6 ||
		clock.now.Sub(run.StartedAt) <= 0 || clock.now.Sub(run.StartedAt) > time.Second {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
	for _, step := range run.Steps {
		if step.Result != gnosis.ResultPass || len(step.Screenshot) == 0 {
			t.Fatalf("step = %+v", step)
		}
	}
	for _, test := range []struct {
		name string
		step gnosis.Step
		fail string
		want gnosis.Result
	}{
		{"precondition", gnosis.Step{Precondition: &gnosis.Assertion{Absent: &target}}, "", gnosis.ResultBlocked},
		{"expectation", gnosis.Step{Expect: &gnosis.Assertion{AttrEquals: &gnosis.AttrEquals{Locator: target, Attribute: "enabled", Value: false}}}, "", gnosis.ResultFail},
		{"read failure", gnosis.Step{Expect: &gnosis.Assertion{Exists: &target}}, "tree 1", gnosis.ResultError},
		{"precondition only", gnosis.Step{Precondition: &gnosis.Assertion{Exists: &target}}, "", gnosis.ResultExecuted},
		{"screenshot overrides false", gnosis.Step{Expect: &gnosis.Assertion{Absent: &target}}, "screenshot", gnosis.ResultError},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := &testDevice{trees: [][]gnosis.Element{tree}, fail: test.fail, fault: errors.New("unreadable")}
			run := runCase(t, d, &testClock{}, test.step)
			if run.Result != test.want || d.reads != 1 || len(run.Steps) != 1 || d.frames != 1 {
				t.Fatalf("run = %+v, operations = %v", run, d.calls)
			}
		})
	}
}

func TestRunPixelAssertionsKeepOnlyTheDecidingFrame(t *testing.T) {
	screen := &gnosis.Assertion{ScreenText: &gnosis.ScreenText{Text: "加载"}, Timeout: time.Second}
	d := &testDevice{regions: [][]gnosis.TextRegion{nil, {{Text: "已加载"}}}}
	run := runCase(t, d, &testClock{}, gnosis.Step{Expect: screen})
	if run.Result != gnosis.ResultPass || d.reads != 0 || d.frames != 2 ||
		!reflect.DeepEqual(run.Steps[0].Screenshot, []byte{2}) {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
	d = &testDevice{}
	clock := &testClock{}
	run = runCase(t, d, clock, gnosis.Step{Expect: screen}, gnosis.Step{Key: &gnosis.Key{Name: "home"}})
	if run.Result != gnosis.ResultFail || len(run.Steps) != 1 || d.frames < 2 ||
		clock.now.Sub(run.StartedAt) != screen.Timeout {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
	d = &testDevice{fail: "ocr", fault: errors.New("unavailable")}
	run = runCase(t, d, &testClock{}, gnosis.Step{Expect: &gnosis.Assertion{Absent: &gnosis.Locator{Text: locatorString("加载")}}})
	if run.Result != gnosis.ResultError || d.recognitions != 1 || d.frames != 1 {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
}

func TestRunTextAssertionsKeepTreeAndPixelsSeparate(t *testing.T) {
	text := "加载"
	d := &testDevice{trees: [][]gnosis.Element{{{Text: "已加载"}}}}
	run := runCase(t, d, &testClock{},
		gnosis.Step{Expect: &gnosis.Assertion{TextContains: &gnosis.TextCheck{
			Locator: gnosis.Locator{Text: locatorString("已加载")}, Text: text,
		}}},
		gnosis.Step{Expect: &gnosis.Assertion{ScreenText: &gnosis.ScreenText{Text: text}}},
	)
	if run.Result != gnosis.ResultFail || run.Steps[0].Result != gnosis.ResultPass ||
		run.Steps[1].Result != gnosis.ResultFail || d.reads != 1 || d.recognitions != 1 {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
}

func TestRunPollReadErrorOverridesAnEarlierPass(t *testing.T) {
	id := "missing"
	d := &testDevice{fail: "tree 3", fault: errors.New("device disconnected")}
	run := runCase(t, d, &testClock{},
		gnosis.Step{Expect: &gnosis.Assertion{Absent: &gnosis.Locator{ID: &id}}},
		gnosis.Step{Expect: &gnosis.Assertion{Exists: &gnosis.Locator{ID: &id}, Timeout: time.Second}},
		gnosis.Step{Key: &gnosis.Key{Name: "home"}},
	)
	if run.Result != gnosis.ResultError || !errors.Is(run.Error, d.fault) || len(run.Steps) != 2 ||
		run.Steps[0].Result != gnosis.ResultPass || run.Steps[1].Result != gnosis.ResultError ||
		d.reads != 3 || d.frames != 2 || slices.Contains(d.calls, "home") {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
}

func TestRunDoesNotRetryAScreenshotNeededToLocateAnElement(t *testing.T) {
	d := &testDevice{fail: "screenshot", fault: errors.New("capture failed")}
	run := runCase(t, d, &testClock{}, gnosis.Step{Locator: gnosis.Locator{Text: locatorString("加载")}})
	if run.Result != gnosis.ResultError || !errors.Is(run.Error, d.fault) || d.frames != 1 || d.recognitions != 0 ||
		!slices.Equal(d.calls, []string{"occupy", "install", "tree 1", "screenshot", "release"}) {
		t.Fatalf("run = %+v, operations = %v", run, d.calls)
	}
}
