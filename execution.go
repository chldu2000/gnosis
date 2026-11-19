package gnosis

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

type ExecutionState string

const (
	ExecutionRunning     ExecutionState = "running"
	ExecutionPausing     ExecutionState = "pausing"
	ExecutionPaused      ExecutionState = "paused"
	ExecutionCancelling  ExecutionState = "cancelling"
	ExecutionFinishing   ExecutionState = "finishing"
	ExecutionCancelled   ExecutionState = "cancelled"
	ExecutionFinished    ExecutionState = "finished"
	ExecutionInterrupted ExecutionState = "interrupted"
)

type CaseProgress struct {
	Name       string
	State      ExecutionState
	Result     Result
	ReportPath string
	Diagnostic string
}

// ExecutionProgress contains public diagnostics and report paths. Images and
// model observations remain in the case reports on the execution host.
type ExecutionProgress struct {
	Sequence         uint64
	State            ExecutionState
	Case, Step, Turn int
	StepName         string
	Cases            []CaseProgress
	Diagnostic       string
}

// ExecutionControl owns the pause, cancellation and progress state of one execution.
type ExecutionControl struct {
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	progress    ExecutionProgress
	changed     chan struct{}
	history     []ExecutionProgress
	pauseLimit  time.Duration
	pauseTimer  *time.Timer
	pausedAt    time.Time
	pausedTotal time.Duration
	pauseCount  uint64
	sealed      bool
}

func NewExecutionControl(pauseLimit time.Duration) *ExecutionControl {
	if pauseLimit <= 0 {
		pauseLimit = 30 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ExecutionControl{
		ctx: ctx, cancel: cancel, changed: make(chan struct{}), pauseLimit: pauseLimit,
		progress: ExecutionProgress{State: ExecutionRunning},
	}
}

// WithProgressPersistence writes an execution checkpoint at case boundaries.
// Returning an error stops the execution before the next opportunity to act.
func WithProgressPersistence(save func(ExecutionProgress) error) RunOption {
	return func(settings *runSettings) { settings.persist = save }
}

func (c *ExecutionControl) Context() context.Context {
	if c == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *ExecutionControl) Snapshot() ExecutionProgress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// Watch returns the next retained update or the current snapshot if updates
// have expired. Cancel the watch context to detach without affecting execution.
func (c *ExecutionControl) Watch(ctx context.Context, after uint64) (ExecutionProgress, error) {
	for {
		c.mu.Lock()
		if c.progress.Sequence > after {
			progress := c.progress
			if len(c.history) > 0 && after >= c.history[0].Sequence-1 {
				for _, event := range c.history {
					if event.Sequence > after {
						progress = event
						break
					}
				}
			}
			progress.Cases = slices.Clone(progress.Cases)
			c.mu.Unlock()
			return progress, nil
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ExecutionProgress{}, ctx.Err()
		}
	}
}

func (c *ExecutionControl) Control(command string) (ExecutionProgress, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch command {
	case "pause":
		if !c.sealed && c.progress.State == ExecutionRunning {
			c.progress.State = ExecutionPausing
			c.publishLocked()
		}
	case "resume":
		if c.progress.State == ExecutionPausing || c.progress.State == ExecutionPaused {
			c.unpauseLocked()
			c.progress.State = ExecutionRunning
			c.publishLocked()
		}
	case "cancel":
		c.cancelLocked()
	default:
		return c.snapshotLocked(), errors.New("unknown execution control command")
	}
	return c.snapshotLocked(), nil
}

func (c *ExecutionControl) unpauseLocked() {
	if c.pauseTimer != nil {
		c.pauseTimer.Stop()
		c.pauseTimer = nil
	}
	if !c.pausedAt.IsZero() {
		c.pausedTotal += time.Since(c.pausedAt)
		c.pausedAt = time.Time{}
	}
}

func (c *ExecutionControl) cancelLocked() {
	if c.sealed || c.ctx.Err() != nil {
		return
	}
	c.unpauseLocked()
	c.progress.State = ExecutionCancelling
	c.cancel()
	c.publishLocked()
}

func (c *ExecutionControl) checkpoint() error {
	if c == nil {
		return nil
	}
	for {
		c.mu.Lock()
		if c.progress.State == ExecutionPausing {
			c.pausedAt = time.Now()
			c.pauseCount++
			c.progress.State = ExecutionPaused
			var timer *time.Timer
			timer = time.AfterFunc(c.pauseLimit, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if c.pauseTimer == timer && c.progress.State == ExecutionPaused {
					c.progress.Diagnostic = "pause time limit reached"
					c.cancelLocked()
				}
			})
			c.pauseTimer = timer
			c.publishLocked()
		}
		paused := c.progress.State == ExecutionPaused
		changed := c.changed
		c.mu.Unlock()
		if !paused {
			return c.ctx.Err()
		}
		select {
		case <-changed:
		case <-c.ctx.Done():
			return c.ctx.Err()
		}
	}
}

func (c *ExecutionControl) pauseTime() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pausedTotal
}

func (c *ExecutionControl) pauseGeneration() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pauseCount
}

func (c *ExecutionControl) wait(duration time.Duration) error {
	for {
		if err := c.checkpoint(); err != nil || duration <= 0 {
			return err
		}
		c.mu.Lock()
		changed := c.changed
		ready := c.progress.State == ExecutionRunning
		c.mu.Unlock()
		if !ready {
			continue
		}
		began := time.Now()
		timer := time.NewTimer(duration)
		select {
		case <-timer.C:
			return c.checkpoint()
		case <-changed:
			timer.Stop()
			duration -= time.Since(began)
		}
	}
}

func (c *ExecutionControl) mark(update func(*ExecutionProgress)) {
	if c != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		update(&c.progress)
		c.publishLocked()
	}
}

func (c *ExecutionControl) publishLocked() {
	c.progress.Sequence++
	c.history = append(c.history, c.snapshotLocked())
	if len(c.history) > 128 {
		c.history[0] = ExecutionProgress{}
		c.history = c.history[1:]
	}
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *ExecutionControl) snapshotLocked() ExecutionProgress {
	progress := c.progress
	progress.Cases = slices.Clone(progress.Cases)
	return progress
}

func (c *ExecutionControl) seal() {
	if c != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.sealed = true
		if c.ctx.Err() == nil {
			c.progress.State = ExecutionFinishing
			c.publishLocked()
		}
	}
}

func (c *ExecutionControl) finish(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sealed = true
	c.unpauseLocked()
	if c.ctx.Err() != nil {
		c.progress.State = ExecutionCancelled
	} else {
		c.progress.State = ExecutionFinished
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		c.progress.Diagnostic = err.Error()
	}
	c.publishLocked()
	c.cancel()
}
