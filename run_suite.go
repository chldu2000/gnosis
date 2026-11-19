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

	settings := runConfiguration(options)
	var records []RunRecord
	for i, testCase := range suite.Cases {
		if i != 0 && !device.Online() {
			return records, fmt.Errorf("suite case %d could not start: %w", i+1, ErrDeviceOffline)
		}
		record, err := executeAndReport(testCase, device, clock, settings)
		records = append(records, *record)
		if err != nil {
			return records, fmt.Errorf("suite case %d: %w", i+1, err)
		}
		if record.Result == ResultError {
			break
		}
	}
	return records, nil
}
