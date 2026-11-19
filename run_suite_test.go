package gnosis_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gnosis"
)

func TestRunSuiteKeepsTheDeviceAndReportsInReferenceOrder(t *testing.T) {
	assets, reports := t.TempDir(), t.TempDir()
	for name, content := range map[string]string{
		"suite.yaml":   "cases: [fail.yaml, blocked.yaml, repeat.yaml, repeat.yaml, error.yaml, later.yaml]\n",
		"fail.yaml":    "name: First failure\nsteps: [{expect: {exists: {id: absent}, timeout: 0ms}}]\n",
		"blocked.yaml": "name: Then blocked\nsteps: [{precondition: {exists: {id: absent}, timeout: 0ms}}]\n",
		"repeat.yaml":  "name: Repeated case\nsteps: [{wait: {duration: 6ms}}]\n",
		"error.yaml":   "name: Device error\nsteps: [{key: {name: home}}]\n",
		"later.yaml":   "name: Must not start\nsteps: [{key: {name: back}}]\n",
	} {
		if err := os.WriteFile(filepath.Join(assets, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, suite, err := gnosis.Load(filepath.Join(assets, "suite.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	clock := &testClock{now: time.Unix(100, 0)}
	d := &imageDevice{testDevice: &testDevice{fail: "home", fault: errors.New("device failed")}}
	competing := &testDevice{}
	prepared := 0
	d.onPrepare = func() {
		prepared++
		if !d.occupied {
			t.Error("suite let go of the device during a case")
		}
		record, err := gnosis.Run(gnosis.Case{}, competing, clock)
		if record != nil || !errors.Is(err, gnosis.ErrRunInProgress) || len(competing.calls) != 0 {
			t.Errorf("competing run = %+v, %v, calls %v", record, err, competing.calls)
		}
	}
	runs, err := gnosis.RunSuite(*suite, d, clock, gnosis.WithReports(reports))
	wantResults := []gnosis.Result{gnosis.ResultFail, gnosis.ResultBlocked, gnosis.ResultExecuted, gnosis.ResultExecuted, gnosis.ResultError}
	wantNames := []string{"First failure", "Then blocked", "Repeated case", "Repeated case", "Device error"}
	if err != nil || len(runs) != len(wantResults) || prepared != len(runs) || d.occupied {
		t.Fatalf("suite = %+v, error = %v, prepared=%d, occupied=%v", runs, err, prepared, d.occupied)
	}
	paths := map[string]bool{}
	for i, run := range runs {
		if run.CaseName != wantNames[i] || run.Result != wantResults[i] || run.DeviceID != d.ID() || paths[run.ReportPath] {
			t.Fatalf("unexpected case %d: %+v", i, run)
		}
		paths[run.ReportPath] = true
		body := readReport(t, run.ReportPath)
		requireInOrder(t, body, run.CaseName, d.ID(), string(run.Result))
	}
	if runs[3].StartedAt.Sub(runs[2].StartedAt) != 6*time.Millisecond || slices.Contains(d.calls, "back") ||
		d.calls[0] != "occupy" || d.calls[len(d.calls)-1] != "release" {
		t.Fatalf("independent start times or device lifetime not respected: %v", d.calls)
	}
	if count := len(slices.DeleteFunc(slices.Clone(d.calls), func(call string) bool {
		return call != "occupy" && call != "release"
	})); count != 2 {
		t.Fatal("the device was acquired or released more than once")
	}
	d.onPrepare = nil
	record, err := gnosis.Run(gnosis.Case{}, d, clock)
	if err != nil || record == nil {
		t.Fatalf("device was not available after the suite: %v", err)
	}
}

func TestRunSuiteRestoresInputMethodBetweenCases(t *testing.T) {
	d := &imageDevice{testDevice: &testDevice{
		changeIME: true, fail: "restore", fault: errors.New("restore failed"),
	}}
	cases := []gnosis.Case{
		{Name: "typed", Steps: []gnosis.Step{{Type: &gnosis.Type{Text: "test"}}}},
		{Name: "untouched", Steps: []gnosis.Step{{Key: &gnosis.Key{Name: "back"}}}},
	}
	runs, err := gnosis.RunSuite(gnosis.Suite{Cases: cases}, d, &testClock{}, gnosis.WithReports(t.TempDir()))
	if err != nil || len(runs) != 2 || runs[0].IMENote == "" || runs[1].IMENote != "" ||
		runs[0].Result != gnosis.ResultExecuted || runs[1].Result != gnosis.ResultExecuted {
		t.Fatalf("suite = %+v, %v", runs, err)
	}
	want := []string{"occupy", "install", "text test", "screenshot", "restore", "back", "screenshot", "release"}
	if !slices.Equal(d.calls, want) {
		t.Fatalf("calls = %v, want %v", d.calls, want)
	}
	if !strings.Contains(readReport(t, runs[0].ReportPath), "restore failed") ||
		strings.Contains(readReport(t, runs[1].ReportPath), "restore failed") {
		t.Fatal("input method annotation was lost or carried into the following case")
	}
}

type disconnectingSuiteDevice struct {
	*imageDevice
	onlineChecks int
}

func (d *disconnectingSuiteDevice) Online() bool {
	d.onlineChecks++
	return d.onlineChecks == 1
}

func TestRunSuiteKeepsReportsWhenDeviceDisconnectsBetweenCases(t *testing.T) {
	d := &disconnectingSuiteDevice{imageDevice: &imageDevice{testDevice: &testDevice{}}}
	suite := gnosis.Suite{Cases: []gnosis.Case{{Name: "started"}, {Name: "never started"}}}
	root := t.TempDir()
	runs, err := gnosis.RunSuite(suite, d, &testClock{}, gnosis.WithReports(root))
	if !errors.Is(err, gnosis.ErrDeviceOffline) || len(runs) != 1 || runs[0].CaseName != "started" || d.occupied {
		t.Fatalf("suite = %+v, %v, occupied=%v", runs, err, d.occupied)
	}
	readReport(t, runs[0].ReportPath)
	items, err := os.ReadDir(root)
	if err != nil || len(items) != 1 {
		t.Fatalf("report directory = %v, %v", items, err)
	}
}

func TestRunSuiteKeepsEarlierReportsWhenALaterSaveFails(t *testing.T) {
	d := &imageDevice{testDevice: &testDevice{}}
	root := filepath.Join(t.TempDir(), "reports")
	saved := root + "-kept"
	second := false
	d.onPrepare = func() {
		if !second {
			second = true
			return
		}
		// Temporarily make the report root unwritable without changing the
		// first report's contents. A regular file is portable across hosts.
		if err := os.Rename(root, saved); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte("blocks report folder creation"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	suite := gnosis.Suite{Cases: []gnosis.Case{{Name: "saved"}, {Name: "write fails"}, {Name: "never starts"}}}
	runs, err := gnosis.RunSuite(suite, d, &testClock{}, gnosis.WithReports(root))
	if err == nil || !strings.Contains(err.Error(), "save report") || len(runs) != 2 ||
		runs[0].ReportPath == "" || runs[1].ReportPath != "" || runs[1].Result != gnosis.ResultExecuted || d.occupied {
		t.Fatalf("suite = %+v, %v, occupied=%v", runs, err, d.occupied)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readReport(t, runs[0].ReportPath), "saved") {
		t.Fatal("earlier report was not retained")
	}
}

func TestRunSuiteRefusesBeforeStartingWithoutAReport(t *testing.T) {
	for _, test := range []struct {
		name  string
		dev   *testDevice
		empty bool
		err   error
	}{
		{"empty", &testDevice{}, true, gnosis.ErrEmptySuite},
		{"offline", &testDevice{offline: true}, false, gnosis.ErrDeviceOffline},
		{"occupied", &testDevice{occupied: true}, false, gnosis.ErrDeviceOccupied},
	} {
		t.Run(test.name, func(t *testing.T) {
			suite := gnosis.Suite{}
			if !test.empty {
				suite.Cases = []gnosis.Case{{Name: "one"}}
			}
			root := filepath.Join(t.TempDir(), "reports")
			runs, err := gnosis.RunSuite(suite, test.dev, &testClock{}, gnosis.WithReports(root))
			if len(runs) != 0 || !errors.Is(err, test.err) || len(test.dev.calls) != 0 {
				t.Fatalf("suite = %+v, %v, calls=%v", runs, err, test.dev.calls)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("no report folder should exist")
			}
		})
	}
}
