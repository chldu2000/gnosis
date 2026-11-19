package localrun

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gnosis"
)

func TestManagerSnapshotsLoadedSuiteSourcesInReferenceOrder(t *testing.T) {
	config, first, _ := fixture(t)
	dir := filepath.Dir(first)
	second := filepath.Join(dir, "other.yaml")
	secondYAML := []byte("name: wait\nsteps: []\n")
	if err := os.WriteFile(second, secondYAML, 0600); err != nil {
		t.Fatal(err)
	}
	suite := filepath.Join(dir, "suite.yaml")
	if err := os.WriteFile(suite, []byte("cases: [wait.yaml, other.yaml, wait.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := OpenManager(config)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := m.Start(StartRequest{RequestID: "suite-request", Path: suite, DeviceID: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = m.Control(accepted.ID, "cancel")
		await(t, m, accepted.ID, func(s Snapshot) bool { return !s.Active() })
	})
	sources := accepted.CaseSources
	if !accepted.Suite || len(sources) != 3 || sources[0].Path != first || sources[0] != sources[2] ||
		sources[1].Path != second || sources[1].Name != sources[0].Name || sources[1].Digest != sha256.Sum256(secondYAML) ||
		sources[0].Steps != 1 || sources[1].Steps != 0 {
		t.Fatalf("suite sources: %+v", sources)
	}
	if err := os.WriteFile(second, []byte("name: changed\nsteps: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := m.Get(accepted.ID, "")
	if err != nil || len(snapshot.CaseSources) != 3 || snapshot.CaseSources[1] != sources[1] {
		t.Fatalf("the source changed after loading: %+v, %v", snapshot.CaseSources, err)
	}
}

type fakeDevice struct {
	gnosis.Device
	serial  string
	offline bool
}

func (d fakeDevice) ID() string                { return d.serial }
func (d fakeDevice) Online() bool              { return !d.offline }
func (fakeDevice) Occupied() bool              { return false }
func (fakeDevice) TryOccupy() bool             { return true }
func (fakeDevice) Release()                    {}
func (fakeDevice) EnsureAutomation() error     { return nil }
func (fakeDevice) Screenshot() ([]byte, error) { return []byte("fixture"), nil }

func fixture(t *testing.T) (Config, string, *atomic.Int32) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "cases")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "wait.yaml")
	if err := os.WriteFile(path, []byte("steps: [{wait: {duration: 4s}}]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opens := &atomic.Int32{}
	conf := Config{
		StatePath: filepath.Join(base, "execution.json"), ReportDir: filepath.Join(base, "reports"),
		OpenDevice: func(serial string) (gnosis.Device, error) {
			opens.Add(1)
			return fakeDevice{serial: serial, offline: serial != "fixture"}, nil
		},
		Model: func() (gnosis.ModelConfig, error) { return gnosis.ModelConfig{}, nil },
	}
	return conf, path, opens
}

func await(t *testing.T, m *Manager, id string, match func(Snapshot) bool) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := m.Get(id, "")
	for err == nil && !match(snapshot) {
		snapshot, err = m.Watch(ctx, id, snapshot.Progress.Sequence)
	}
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestManagerRestoresAcceptedRequestAndKeepsOneExecution(t *testing.T) {
	config, path, opens := fixture(t)
	m, err := OpenManager(config)
	if err != nil {
		t.Fatal(err)
	}
	request := StartRequest{RequestID: "request-1", Path: path, DeviceID: "fixture"}
	accepted, err := m.Start(request)
	if err != nil || accepted.ID == "" {
		t.Fatalf("start: %+v, %v", accepted, err)
	}
	t.Cleanup(func() { _, _ = m.Control(accepted.ID, "cancel") })
	reattached, err := m.Get("", request.RequestID)
	if err != nil || accepted.ID != reattached.ID || !reattached.Active() {
		t.Fatalf("startup reply recovery: %+v, %v", reattached, err)
	}
	duplicate, err := m.Start(request)
	if err != nil || duplicate.ID != accepted.ID || opens.Load() != 1 {
		t.Fatalf("duplicate request repeated device work: %+v, %v", duplicate, err)
	}
	if _, err := m.Start(StartRequest{RequestID: "other-request", Path: path, DeviceID: "fixture"}); !errors.Is(err, gnosis.ErrRunInProgress) {
		t.Fatalf("simultaneous start: %v", err)
	}
	await(t, m, accepted.ID, func(s Snapshot) bool { return s.Progress.Step == 1 })
	if _, err := m.Control(accepted.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	paused := await(t, m, accepted.ID, func(s Snapshot) bool { return s.Progress.State == gnosis.ExecutionPaused })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Watch(ctx, paused.ID, paused.Progress.Sequence); !errors.Is(err, context.Canceled) {
		t.Fatalf("closing a subscription returned %v", err)
	}
	if state, err := m.Get("", ""); err != nil || state.Progress.State != gnosis.ExecutionPaused {
		t.Fatalf("subscription changed the execution: %+v, %v", state, err)
	}
	if _, err := m.Control(accepted.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	finished := await(t, m, accepted.ID, func(s Snapshot) bool { return !s.Active() })
	for deadline := time.Now().Add(time.Second); !m.Idle(); {
		if time.Now().After(deadline) {
			t.Fatal("terminal snapshot was not persisted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	m, err = OpenManager(config)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := m.Get("", "")
	if err != nil || restored.Progress.State != gnosis.ExecutionCancelled || restored.ID != finished.ID || len(restored.Progress.Cases) != 1 || restored.Progress.ReportPath == "" {
		t.Fatalf("terminal report not restored: %+v, %v", restored, err)
	}
	if _, err := os.Stat(restored.Progress.ReportPath); err != nil {
		t.Fatal(err)
	}
}

func TestManagerMarksOrphanedExecutionInterruptedAndRefusesBeforeStarting(t *testing.T) {
	config, path, opens := fixture(t)
	prior := Snapshot{ID: "earlier-process", Request: StartRequest{RequestID: "original"}, Suite: true, Progress: gnosis.ExecutionProgress{
		State: gnosis.ExecutionPaused, Case: 2, Cases: []gnosis.CaseProgress{
			{Name: "done", Result: gnosis.ResultPass, State: gnosis.ExecutionFinished, ReportPath: "completed.html"},
			{Name: "in progress", State: gnosis.ExecutionRunning},
		},
	}}
	if err := WriteJSON(config.StatePath, prior); err != nil {
		t.Fatal(err)
	}
	m, err := OpenManager(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(StartRequest{RequestID: "missing", Path: path + ".missing", DeviceID: "fixture"}); err == nil || opens.Load() != 0 {
		t.Fatalf("bad path reached the device: %v", err)
	}
	if _, err := m.Start(StartRequest{RequestID: "offline", Path: path, DeviceID: "offline"}); !errors.Is(err, gnosis.ErrDeviceOffline) {
		t.Fatalf("offline start: %v", err)
	}
	restored, err := m.Get("", "original")
	if err != nil || restored.Progress.State != gnosis.ExecutionInterrupted || restored.Progress.Cases[0] != prior.Progress.Cases[0] ||
		restored.Progress.Cases[1].State != gnosis.ExecutionInterrupted || restored.Progress.Cases[1].Result != "" {
		t.Fatalf("interruption lost or fabricated a result: %+v, %v", restored, err)
	}
	if _, err := m.Get("", "offline"); !errors.Is(err, ErrUnknownExecution) {
		t.Fatalf("refused request was started: %v", err)
	}
}
