package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gnosis"
)

func main() {
	host := commandHost{
		openDevice: func(serial string) (gnosis.Device, error) {
			return gnosis.NewAndroidDevice(serial)
		},
		reportDirectory: defaultReportDirectory,
		clock:           gnosis.RealClock{},
	}
	os.Exit(host.run(os.Args[1:], os.Stdout, os.Stderr))
}

type commandHost struct {
	openDevice      func(string) (gnosis.Device, error)
	reportDirectory func() (string, error)
	clock           gnosis.Clock
}

func defaultReportDirectory() (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate report directory: %w", err)
	}
	return filepath.Join(directory, "gnosis", "reports"), nil
}

func (host commandHost) run(args []string, stdout, stderr io.Writer) int {
	config := gnosis.ModelConfig{
		Endpoint: os.Getenv("GNOSIS_MODEL_ENDPOINT"),
		Name:     os.Getenv("GNOSIS_MODEL_NAME"),
		APIKey:   os.Getenv("GNOSIS_MODEL_API_KEY"),
	}
	diagnose := func(message string) {
		if config.APIKey != "" {
			message = strings.ReplaceAll(message, config.APIKey, "[redacted]")
		}
		fmt.Fprintln(stderr, message)
	}
	flags := flag.NewFlagSet("gnosis", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	deviceID := flags.String("device", "", "Android device serial")
	reportDir := flags.String("report-dir", "", "directory for this execution's report")
	flags.StringVar(&config.Endpoint, "model-endpoint", config.Endpoint, "model endpoint for this invocation")
	flags.StringVar(&config.Name, "model", config.Name, "model name for this invocation")
	const usage = "usage: gnosis --device SERIAL [--report-dir DIR] [--model-endpoint URL] [--model NAME] FILE"
	flags.Usage = func() { diagnose(usage) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		diagnose(err.Error())
		diagnose(usage)
		return 1
	}
	if strings.TrimSpace(*deviceID) == "" || flags.NArg() != 1 {
		diagnose("an explicit device serial and one case or suite file are required")
		diagnose(usage)
		return 1
	}

	source := flags.Arg(0)
	root := *reportDir
	if root == "" {
		defaultRoot, err := host.reportDirectory()
		if err != nil {
			diagnose(err.Error())
			return 1
		}
		root = defaultRoot
	}
	var sources []gnosis.CaseSource
	testCase, suite, err := gnosis.Load(source, gnosis.ReportsOutsideCaseTree(root), gnosis.WithCaseSources(&sources))
	if err != nil {
		diagnose(err.Error())
		return 1
	}
	device, err := host.openDevice(*deviceID)
	if err != nil {
		diagnose(err.Error())
		return 1
	}

	options := []gnosis.RunOption{gnosis.WithReports(root), gnosis.WithReportSource(source, sources), gnosis.WithModel(config)}
	var records []gnosis.RunRecord
	if suite == nil {
		var record *gnosis.RunRecord
		record, err = gnosis.Run(testCase, device, host.clock, options...)
		if record != nil {
			records = append(records, *record)
		}
	} else {
		records, err = gnosis.RunSuite(*suite, device, host.clock, options...)
	}

	exit := 0
	reportPath := ""
	for _, record := range records {
		if reportPath == "" {
			reportPath = record.ReportPath
		}
		if record.Result != gnosis.ResultPass && record.Result != gnosis.ResultExecuted {
			exit = 1
			message := fmt.Sprintf("%s: %s", record.CaseName, record.Result)
			if record.Error != nil {
				message += ": " + record.Error.Error()
			}
			diagnose(message)
		}
	}
	if reportPath != "" {
		if _, outputErr := fmt.Fprintln(stdout, reportPath); outputErr != nil {
			diagnose("could not print report path: " + outputErr.Error())
			exit = 1
		}
	}
	if err != nil {
		diagnose(err.Error())
		exit = 1
	}
	return exit
}
