package main

import (
	"bufio"
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
		input:           os.Stdin,
	}
	os.Exit(host.run(os.Args[1:], os.Stdout, os.Stderr))
}

type commandHost struct {
	input           io.Reader
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
	review := flags.Bool("draft", false, "review actions after this trial and save a new case")
	flags.StringVar(&config.Endpoint, "model-endpoint", config.Endpoint, "model endpoint for this invocation")
	flags.StringVar(&config.Name, "model", config.Name, "model name for this invocation")
	const usage = "usage: gnosis --device SERIAL [--draft] [--report-dir DIR] [--model-endpoint URL] [--model NAME] FILE"
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
	var draftSource gnosis.DraftSource
	loadOptions := []gnosis.LoadOption{gnosis.ReportsOutsideCaseTree(root), gnosis.WithCaseSources(&sources)}
	if *review {
		loadOptions = append(loadOptions, gnosis.WithDraftSource(&draftSource))
	}
	testCase, suite, err := gnosis.Load(source, loadOptions...)
	if err != nil {
		diagnose(err.Error())
		return 1
	}
	if *review && suite != nil {
		diagnose("草稿仅适用于单用例执行")
		return 1
	}
	device, err := host.openDevice(*deviceID)
	if err != nil {
		diagnose(err.Error())
		return 1
	}

	options := []gnosis.RunOption{gnosis.WithReports(root), gnosis.WithReportSource(source, sources), gnosis.WithModel(config)}
	if *review {
		options = append(options, gnosis.WithCaseDraft(draftSource))
	}
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
	if *review && err == nil && reportPath != "" {
		draft, err := gnosis.ReadCaseDraft(reportPath)
		if err != nil {
			diagnose(err.Error())
			return 1
		}
		for _, turn := range draft.Turns {
			fmt.Fprintf(stderr, "步骤 %d · AI %d/%d · %s · %s\n", turn.Row+1, turn.Step, turn.Turn, turn.Goal, turn.Note)
			if turn.Coordinate {
				fmt.Fprintln(stderr, "  请复核坐标点击")
			}
		}
		fmt.Fprintln(stderr, draft.YAML)
		fmt.Fprintln(stderr, "查看会话报告中的截图后，输入新 YAML 路径以保存（留空放弃）：")
		if host.input != nil {
			line, readErr := bufio.NewReader(host.input).ReadString('\n')
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				diagnose(readErr.Error())
				return 1
			}
			if name := strings.TrimSpace(line); name != "" {
				if err := draft.SaveNew(name); err != nil {
					diagnose(err.Error())
					return 1
				}
			}
		}
	}
	return exit
}
