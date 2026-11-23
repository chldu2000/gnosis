package gnosis

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ModelConfig selects the model for one execution. Endpoint is the full
// OpenAI-compatible Chat Completions URL. Timeout applies to each request
// and defaults to 30 seconds when zero.
type ModelConfig struct {
	Endpoint string
	Name     string
	APIKey   string
	Timeout  time.Duration
}

// WithModel provides host model settings. Run uses them only when it reaches an
// AI step. Keep credentials in the calling environment, separate from cases.
func WithModel(config ModelConfig) RunOption {
	return func(settings *runSettings) { settings.model = config }
}

func (r *caseRun) runAI(step AI, observation *frame) ([]TrajectoryTurn, *frame, error) {
	model, err := newStepModel(r.settings.model)
	if err != nil {
		return nil, observation, err
	}
	if step.MaxSteps < 1 {
		return nil, observation, errors.New("AI step requires a positive step limit")
	}

	var trajectory []TrajectoryTurn
	control := r.settings.control
	for len(trajectory) < step.MaxSteps {
		pausedBefore := control.pauseGeneration()
		if err := control.checkpoint(); err != nil {
			return trajectory, observation, err
		}
		if pausedBefore != control.pauseGeneration() {
			observation = &frame{device: r.device}
		}
		pausedBefore = control.pauseGeneration()
		tree, err := r.device.ControlTree()
		if err != nil {
			return trajectory, observation, fmt.Errorf("observe AI step control tree: %w", err)
		}
		screenshot, err := observation.screenshot()
		if err != nil {
			return trajectory, observation, err
		}
		choice, err := model.decide(control.Context(), step.Goal, tree, screenshot)
		if err != nil {
			return trajectory, observation, err
		}
		if err := control.checkpoint(); err != nil {
			return trajectory, observation, err
		}
		if pausedBefore != control.pauseGeneration() {
			observation = &frame{device: r.device}
			continue
		}
		if choice.stop {
			return trajectory, observation, nil
		}

		// Locator fallback should inspect the device at dispatch time. The
		// model's earlier screenshot can be stale after waiting for its reply.
		actionFrame := &frame{device: r.device}
		var before *InterfaceCapture
		if r.settings.draftSource != nil && choice.action.TapAt != nil {
			// The model's earlier observation may predate a screen transition.
			// A missing review attachment does not change the action's outcome.
			before, _ = observeInterface(r.device)
		}
		dispatched, actionErr := r.action(choice.action, actionFrame)
		if !dispatched {
			if actionFrame.taken {
				observation = actionFrame
			}
			return trajectory, observation, actionErr
		}
		observation = &frame{device: r.device}
		var endScreenshot []byte
		var captureErr error
		if control.Context().Err() == nil {
			endScreenshot, captureErr = observation.screenshot()
		}
		turn := TrajectoryTurn{Action: choice.action, Note: choice.note, Screenshot: endScreenshot}
		if r.settings.draftSource != nil {
			turn.row = choice.row
			turn.before = before
		}
		trajectory = append(trajectory, turn)
		control.mark(func(progress *ExecutionProgress) { progress.Turn = len(trajectory) })
		if actionErr != nil {
			return trajectory, observation, actionErr
		}
		if captureErr != nil {
			return trajectory, observation, captureErr
		}
	}
	return trajectory, observation, nil
}

func safeRunError(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	message := err.Error()
	if redacted := publicRunText(message, secret); redacted != message {
		return errors.New(redacted)
	}
	return err
}

func publicRunText(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[redacted]")
}
