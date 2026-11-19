// Package localrun owns local execution identity and lifecycle. The desktop
// connects to this code in a separate process via the Windows named pipe.
package localrun

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gnosis"
	"gnosis/internal/fileio"
)

type StartRequest struct {
	RequestID string
	Path      string
	DeviceID  string
}

type Snapshot struct {
	ID          string
	Request     StartRequest
	Suite       bool
	Progress    gnosis.ExecutionProgress
	CaseSources []gnosis.CaseSource `json:",omitempty"`
}

func (s Snapshot) Active() bool {
	switch s.Progress.State {
	case gnosis.ExecutionRunning, gnosis.ExecutionPausing, gnosis.ExecutionPaused, gnosis.ExecutionCancelling, gnosis.ExecutionFinishing:
		return true
	default:
		return false
	}
}

type Config struct {
	StatePath  string
	ReportDir  string
	PauseLimit time.Duration
	OpenDevice func(string) (gnosis.Device, error)
	Model      func() (gnosis.ModelConfig, error)
}

var ErrUnknownExecution = errors.New("execution not found")

type Manager struct {
	config   Config
	start    sync.Mutex
	mu       sync.Mutex
	latest   Snapshot
	control  *gnosis.ExecutionControl
	starting bool
}

func OpenManager(config Config) (*Manager, error) {
	m := &Manager{config: config}
	if data, err := os.ReadFile(config.StatePath); err == nil {
		if err := json.Unmarshal(data, &m.latest); err != nil {
			return nil, fmt.Errorf("read execution snapshot: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// Opening the store is exclusive to a new execution host. An unfinished
	// snapshot therefore belongs to an earlier host that is no longer alive.
	if m.latest.Active() {
		m.latest.Progress.State = gnosis.ExecutionInterrupted
		m.latest.Progress.Sequence++
		for i := range m.latest.Progress.Cases {
			item := &m.latest.Progress.Cases[i]
			if item.State == gnosis.ExecutionRunning {
				item.State = gnosis.ExecutionInterrupted
			}
		}
		if err := m.save(m.latest); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) Start(request StartRequest) (Snapshot, error) {
	if strings.TrimSpace(request.RequestID) == "" || len(request.RequestID) > 128 ||
		strings.TrimSpace(request.Path) == "" || strings.TrimSpace(request.DeviceID) == "" {
		return Snapshot{}, errors.New("request ID, case or suite file, and device are required")
	}
	if !m.start.TryLock() {
		return Snapshot{}, gnosis.ErrRunInProgress
	}
	defer m.start.Unlock()
	if prior, err := m.Get("", request.RequestID); err == nil {
		if prior.Request != request {
			return Snapshot{}, errors.New("request ID already identifies a different start request")
		}
		return prior, nil
	}
	m.mu.Lock()
	if m.control != nil {
		m.mu.Unlock()
		return Snapshot{}, gnosis.ErrRunInProgress
	}
	m.starting = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.starting = false
		m.mu.Unlock()
	}()

	path, err := filepath.Abs(request.Path)
	if err != nil {
		return Snapshot{}, err
	}
	var sources []gnosis.CaseSource
	testCase, suite, err := gnosis.Load(path, gnosis.ReportsOutsideCaseTree(m.config.ReportDir), gnosis.WithCaseSources(&sources))
	if err != nil {
		return Snapshot{}, err
	}
	model, err := m.config.Model()
	if err != nil {
		return Snapshot{}, err
	}
	device, err := m.config.OpenDevice(request.DeviceID)
	if err != nil {
		return Snapshot{}, err
	}
	control := gnosis.NewExecutionControl(m.config.PauseLimit)
	initial := Snapshot{ID: rand.Text(), Request: request, Suite: suite != nil, Progress: control.Snapshot(), CaseSources: sources}
	type response struct {
		snapshot Snapshot
		err      error
	}
	started := make(chan response, 1)
	go func() {
		accepted := false
		err := gnosis.RunControlled(testCase, suite, device, gnosis.RealClock{}, control, func() error {
			if err := m.save(initial); err != nil {
				return err
			}
			m.mu.Lock()
			m.latest, m.control = initial, control
			m.mu.Unlock()
			accepted = true
			started <- response{snapshot: initial}
			return nil
		}, gnosis.WithReports(m.config.ReportDir), gnosis.WithModel(model), gnosis.WithProgressPersistence(func(progress gnosis.ExecutionProgress) error {
			current := initial
			current.Progress = progress
			return m.save(current)
		}))
		if !accepted {
			started <- response{err: err}
			return
		}
		final := initial
		final.Progress = control.Snapshot()
		if err := m.save(final); err != nil {
			if final.Progress.Diagnostic != "" {
				final.Progress.Diagnostic += "\n"
			}
			final.Progress.Diagnostic += err.Error()
		}
		m.mu.Lock()
		m.latest, m.control = final, nil
		m.mu.Unlock()
	}()
	result := <-started
	return result.snapshot, result.err
}

func (m *Manager) Get(id, requestID string) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.find(id, requestID)
	if err == nil && m.control != nil {
		current.Progress = m.control.Snapshot()
	}
	return current, err
}

func (m *Manager) find(id, requestID string) (Snapshot, error) {
	if (id != "" && m.latest.ID != id) || (requestID != "" && m.latest.Request.RequestID != requestID) {
		return Snapshot{}, ErrUnknownExecution
	}
	return m.latest, nil
}

func (m *Manager) Watch(ctx context.Context, id string, after uint64) (Snapshot, error) {
	if id == "" {
		return Snapshot{}, ErrUnknownExecution
	}
	m.mu.Lock()
	current, err := m.find(id, "")
	control := m.control
	m.mu.Unlock()
	if err != nil || control == nil {
		return current, err
	}
	current.Progress = control.Snapshot()
	if !current.Active() && current.Progress.Sequence <= after {
		return current, nil
	}
	current.Progress, err = control.Watch(ctx, after)
	return current, err
}

func (m *Manager) Control(id, command string) (Snapshot, error) {
	if id == "" {
		return Snapshot{}, ErrUnknownExecution
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.find(id, "")
	if err != nil || m.control == nil {
		return current, err
	}
	current.Progress, err = m.control.Control(command)
	return current, err
}

func (m *Manager) Idle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.starting && m.control == nil
}

func (m *Manager) save(value Snapshot) error {
	if err := WriteJSON(m.config.StatePath, value); err != nil {
		return fmt.Errorf("save execution snapshot: %w", err)
	}
	return nil
}

func WriteJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return fileio.Replace(path, data)
}
