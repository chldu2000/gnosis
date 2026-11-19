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

type runSettings struct {
	model ModelConfig
}

type RunOption func(*runSettings)

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
	for len(trajectory) < step.MaxSteps {
		tree, err := r.device.ControlTree()
		if err != nil {
			return trajectory, observation, fmt.Errorf("observe AI step control tree: %w", err)
		}
		screenshot, err := observation.screenshot()
		if err != nil {
			return trajectory, observation, err
		}
		choice, err := model.decide(step.Goal, tree, screenshot)
		if err != nil || choice.stop {
			return trajectory, observation, err
		}

		// Locator fallback should inspect the device at dispatch time. The
		// model's earlier screenshot can be stale after waiting for its reply.
		actionFrame := &frame{device: r.device}
		dispatched, actionErr := r.action(choice.action, actionFrame)
		if !dispatched {
			if actionFrame.taken {
				observation = actionFrame
			}
			return trajectory, observation, actionErr
		}
		observation = &frame{device: r.device}
		screenshot, captureErr := observation.screenshot()
		trajectory = append(trajectory, TrajectoryTurn{
			Action: choice.action, Note: choice.note, Screenshot: screenshot,
		})
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
	if secret == "" {
		return err
	}
	message := err.Error()
	if strings.Contains(message, secret) {
		return errors.New(strings.ReplaceAll(message, secret, "[redacted]"))
	}
	return err
}
