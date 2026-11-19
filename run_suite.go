package gnosis

import (
	"errors"
	"fmt"
)

var ErrEmptySuite = errors.New("suite contains no cases")

// RunSuite executes a loaded suite in order under one device reservation.
// It returns records only for cases that started. A case with an error result
// ends the suite. A refusal or interruption returns a separate error.
func RunSuite(suite Suite, device Device, clock Clock, options ...RunOption) ([]RunRecord, error) {
	if len(suite.Cases) == 0 {
		return nil, ErrEmptySuite
	}
	release, err := occupyRunDevice(device)
	if err != nil {
		return nil, err
	}
	defer release()

	return runSuiteCases(suite.Cases, device, clock, runConfiguration(options))
}

// RunControlled extends the ordinary case and suite entry points with local
// execution control. It calls accepted after acquiring the device and before
// starting the first case, allowing the caller to persist its execution ID.
func RunControlled(testCase Case, suite *Suite, device Device, clock Clock, control *ExecutionControl, accepted func() error, options ...RunOption) (err error) {
	if control == nil {
		return errors.New("execution control is required")
	}
	settings := runConfiguration(options)
	settings.control = control
	defer func() { control.finish(safeRunError(err, settings.model.APIKey)) }()
	if suite != nil && len(suite.Cases) == 0 {
		return ErrEmptySuite
	}
	release, err := occupyRunDevice(device)
	if err != nil {
		return err
	}
	defer func() {
		_ = cleanupDevice(device, func() error { release(); return nil })
	}()
	if accepted != nil {
		if err := accepted(); err != nil {
			return err
		}
	}
	setDeviceContext(device, control.Context())
	if suite == nil {
		if err := control.checkpoint(); err != nil {
			return err
		}
		record, err := executeAndReport(testCase, device, clock, settings, 1, true)
		publishCase(control, record)
		return errors.Join(err, settings.persistProgress())
	}
	_, err = runSuiteCases(suite.Cases, device, clock, settings)
	return err
}

func runSuiteCases(cases []Case, device Device, clock Clock, settings runSettings) ([]RunRecord, error) {
	var records []RunRecord
	for i, testCase := range cases {
		if err := settings.control.checkpoint(); err != nil {
			return records, err
		}
		if i != 0 && !device.Online() {
			if err := settings.control.Context().Err(); err != nil {
				return records, err
			}
			return records, fmt.Errorf("suite case %d could not start: %w", i+1, ErrDeviceOffline)
		}
		if err := settings.control.checkpoint(); err != nil {
			return records, err
		}
		record, err := executeAndReport(testCase, device, clock, settings, i+1, i == len(cases)-1)
		records = append(records, *record)
		publishCase(settings.control, record)
		err = errors.Join(err, settings.persistProgress())
		if err != nil {
			return records, fmt.Errorf("suite case %d: %w", i+1, err)
		}
		if record.Result == ResultError || record.State == ExecutionCancelled {
			break
		}
	}
	return records, nil
}

func publishCase(control *ExecutionControl, record *RunRecord) {
	control.mark(func(progress *ExecutionProgress) {
		item := &progress.Cases[len(progress.Cases)-1]
		item.Result, item.State, item.ReportPath = record.Result, record.State, record.ReportPath
		if record.Error != nil {
			item.Diagnostic = record.Error.Error()
		}
	})
}
