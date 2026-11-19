package gnosis_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gnosis"
)

func transition(t *testing.T, control *gnosis.ExecutionControl, command string) gnosis.ExecutionProgress {
	t.Helper()
	progress, err := control.Control(command)
	if err != nil {
		t.Fatal(err)
	}
	return progress
}

func awaitExecution(t *testing.T, control *gnosis.ExecutionControl, match func(gnosis.ExecutionProgress) bool) gnosis.ExecutionProgress {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	progress := control.Snapshot()
	for !match(progress) {
		if progress.State == gnosis.ExecutionFinished || progress.State == gnosis.ExecutionCancelled {
			t.Fatalf("unexpected terminal progress: %+v", progress)
		}
		next, err := control.Watch(ctx, progress.Sequence)
		if err != nil {
			t.Fatalf("last execution progress %+v: %v", progress, err)
		}
		progress = next
	}
	return progress
}

func awaitState(t *testing.T, control *gnosis.ExecutionControl, state gnosis.ExecutionState) gnosis.ExecutionProgress {
	t.Helper()
	return awaitExecution(t, control, func(progress gnosis.ExecutionProgress) bool { return progress.State == state })
}

func controlled(t *testing.T, testCase gnosis.Case, suite *gnosis.Suite, device gnosis.Device, limit time.Duration, opts ...gnosis.RunOption) (*gnosis.ExecutionControl, <-chan error) {
	t.Helper()
	controller := gnosis.NewExecutionControl(limit)
	done := make(chan error, 1)
	go func() {
		done <- gnosis.RunControlled(testCase, suite, device, gnosis.RealClock{}, controller, nil, opts...)
		close(done)
	}()
	t.Cleanup(func() {
		_, _ = controller.Control("cancel")
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("controlled run did not exit")
		}
	})
	return controller, done
}

func TestControlledSuitePausesAndSavesPartialReportAfterCancellation(t *testing.T) {
	device := &imageDevice{testDevice: &testDevice{changeIME: true}}
	suite := &gnosis.Suite{Cases: []gnosis.Case{
		{Name: "first", Steps: []gnosis.Step{{Wait: &gnosis.Wait{}}}},
		{Name: "second", Steps: []gnosis.Step{
			{Name: "already typed", Type: &gnosis.Type{Text: "fixture"}},
			{Name: "still waiting", Wait: &gnosis.Wait{Duration: 3 * time.Second}},
		}},
		{Name: "later", Steps: []gnosis.Step{{Key: &gnosis.Key{Name: "home"}}}},
	}}
	control, done := controlled(t, gnosis.Case{}, suite, device, 0, gnosis.WithReports(t.TempDir()))
	awaitExecution(t, control, func(p gnosis.ExecutionProgress) bool { return p.Case == 2 && p.Step == 2 })
	if state := transition(t, control, "pause").State; state != gnosis.ExecutionPausing {
		t.Fatalf("pause acknowledgment = %s", state)
	}
	paused := awaitState(t, control, gnosis.ExecutionPaused)
	if len(paused.Cases) != 2 || paused.Cases[0].Result != gnosis.ResultExecuted || paused.ReportPath == "" {
		t.Fatalf("lost the first case while paused: %+v", paused)
	}
	if _, err := gnosis.Run(gnosis.Case{}, &testDevice{}, &testClock{}); !errors.Is(err, gnosis.ErrRunInProgress) {
		t.Fatalf("competing run during pause: %v", err)
	}
	transition(t, control, "cancel")
	finished := awaitState(t, control, gnosis.ExecutionCancelled)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	second := finished.Cases[1]
	if len(finished.Cases) != 2 || second.State != gnosis.ExecutionCancelled || second.Result != "" ||
		finished.ReportPath != paused.ReportPath || device.occupied || !slices.Contains(device.calls, "restore") || slices.Contains(device.calls, "home") {
		t.Fatalf("cancelled suite: %+v, device calls %v", finished, device.calls)
	}
	if body := readReport(t, finished.ReportPath); !strings.Contains(body, "already typed") || strings.Contains(body, "still waiting") || !strings.Contains(body, "cancelled") {
		t.Fatalf("partial report was missing completed step or showed unwitnessed result: %s", body)
	}
	if after := transition(t, control, "cancel"); after.State != gnosis.ExecutionCancelled || after.Sequence != finished.Sequence {
		t.Fatalf("late control changed the terminal state: %+v", after)
	}
}

func TestNormalCompletionWinsCancelDuringReportFinalization(t *testing.T) {
	device := &imageDevice{testDevice: &testDevice{}}
	settled, release := make(chan struct{}), make(chan struct{})
	control, done := controlled(t, gnosis.Case{Name: "settled"}, nil, device, 0, gnosis.WithProgressPersistence(func(progress gnosis.ExecutionProgress) error {
		if progress.Cases[0].Result != "" {
			close(settled)
			<-release
		}
		return nil
	}))
	var released bool
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	select {
	case <-settled:
	case <-time.After(time.Second):
		t.Fatal("case conclusion was not available before finalization")
	}
	if state := transition(t, control, "cancel").State; state != gnosis.ExecutionFinishing {
		t.Fatalf("late cancel changed committed outcome: %s", state)
	}
	close(release)
	released = true
	finished := awaitState(t, control, gnosis.ExecutionFinished)
	if err := <-done; err != nil || finished.Cases[0].Result != gnosis.ResultExecuted || device.occupied {
		t.Fatalf("normal completion lost to late cancellation: %+v, %v", finished, err)
	}
}

type assertionPauseDevice struct {
	*imageDevice
	entered chan struct{}
	proceed chan struct{}
}

func (d *assertionPauseDevice) ControlTree() ([]gnosis.Element, error) {
	if d.reads == 0 {
		close(d.entered)
		<-d.proceed
	}
	return d.imageDevice.ControlTree()
}

func TestControlledAssertionKeepsDeadlineAndObservesAgainAfterResume(t *testing.T) {
	const timeout = 110 * time.Millisecond
	id := "ready"
	device := &assertionPauseDevice{
		imageDevice: &imageDevice{testDevice: &testDevice{trees: [][]gnosis.Element{{}, {{ID: id}}}}},
		entered:     make(chan struct{}), proceed: make(chan struct{}),
	}
	control, done := controlled(t, gnosis.Case{Name: "assert", Steps: []gnosis.Step{
		{Expect: &gnosis.Assertion{Exists: &gnosis.Locator{ID: &id}, Timeout: timeout}},
	}}, nil, device, 0)
	select {
	case <-device.entered:
	case <-time.After(time.Second):
		t.Fatal("assertion did not observe the device")
	}
	transition(t, control, "pause")
	close(device.proceed)
	awaitState(t, control, gnosis.ExecutionPaused)
	time.Sleep(2 * timeout)
	transition(t, control, "resume")
	finished := awaitState(t, control, gnosis.ExecutionFinished)
	if err := <-done; err != nil || finished.Cases[0].Result != gnosis.ResultPass || device.reads != 2 {
		t.Fatalf("assertion after resume: %+v, %v, device calls %v", finished, err, device.calls)
	}
}

func TestControlledWaitFreezesAndExpiresAtPauseLimit(t *testing.T) {
	for _, test := range []struct {
		name   string
		limit  time.Duration
		resume bool
	}{
		{"resume", time.Second, true},
		{"expire", 40 * time.Millisecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := &imageDevice{testDevice: &testDevice{}}
			control, done := controlled(t, gnosis.Case{Name: "wait", Steps: []gnosis.Step{{Wait: &gnosis.Wait{Duration: 180 * time.Millisecond}}}}, nil, device, test.limit)
			awaitExecution(t, control, func(p gnosis.ExecutionProgress) bool { return p.Step == 1 })
			transition(t, control, "pause")
			awaitState(t, control, gnosis.ExecutionPaused)
			if test.resume {
				time.Sleep(210 * time.Millisecond)
				if control.Snapshot().State != gnosis.ExecutionPaused {
					t.Fatal("wait expired during pause")
				}
				resumed := time.Now()
				transition(t, control, "resume")
				awaitState(t, control, gnosis.ExecutionFinished)
				if time.Since(resumed) < 90*time.Millisecond {
					t.Fatal("wait lost remaining time after resume")
				}
			} else if p := awaitState(t, control, gnosis.ExecutionCancelled); p.Diagnostic != "pause time limit reached" {
				t.Fatalf("pause expiry diagnostic: %+v", p)
			}
			if err := <-done; err != nil || device.occupied {
				t.Fatalf("device not released: %v", err)
			}
		})
	}
}

func TestControlledAIDiscardsObservationAfterPauseAndCancelsModelRequest(t *testing.T) {
	for _, cancelExecution := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelExecution), func(t *testing.T) {
			var calls atomic.Int32
			entered, respond, requestCancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if calls.Add(1) == 1 {
					close(entered)
					select {
					case <-respond:
						fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"action\":\"key\",\"params\":{\"name\":\"home\"},\"note\":\"stale action\"}"}}]}`)
					case <-r.Context().Done():
						close(requestCancelled)
					}
					return
				}
				fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"stop\":true}"}}]}`)
			}))
			t.Cleanup(server.Close)
			device := &imageDevice{testDevice: &testDevice{}}
			control, done := controlled(t, gnosis.Case{Name: "AI", Steps: []gnosis.Step{withAI("look", 2)}}, nil, device, 0,
				gnosis.WithModel(gnosis.ModelConfig{Endpoint: server.URL, Name: "fixture", APIKey: "simulate-secret"}))
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("model request missing")
			}
			if cancelExecution {
				transition(t, control, "cancel")
				select {
				case <-requestCancelled:
				case <-time.After(time.Second):
					t.Fatal("model request did not receive cancellation")
				}
				awaitState(t, control, gnosis.ExecutionCancelled)
			} else {
				transition(t, control, "pause")
				close(respond)
				awaitState(t, control, gnosis.ExecutionPaused)
				transition(t, control, "resume")
				finished := awaitState(t, control, gnosis.ExecutionFinished)
				if finished.Cases[0].Result != gnosis.ResultExecuted || calls.Load() != 2 {
					t.Fatalf("AI failed to observe again: %+v, calls %d", finished, calls.Load())
				}
			}
			if err := <-done; err != nil || slices.Contains(device.calls, "home") {
				t.Fatalf("stale AI choice reached the device: %v, %v", device.calls, err)
			}
		})
	}
}
