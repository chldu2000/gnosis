package gnosis

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gnosis/internal/fileio"
)

// WithReports saves an offline report for an execution that starts.
// The caller selects a directory outside the case and suite files' tree.
func WithReports(directory string) RunOption {
	return func(settings *runSettings) { settings.reportDirectory = directory }
}

// WithReportSource supplies file names for the report's case index.
// Use the same order as the loaded cases, including repeated references.
func WithReportSource(input string, sources []CaseSource) RunOption {
	return func(settings *runSettings) {
		settings.reportInput, settings.reportSources = input, slices.Clone(sources)
	}
}

// ReportsOutsideCaseTree asks Load to check the intended report directory
// against the input file and every referenced case. It does not create files.
func ReportsOutsideCaseTree(directory string) LoadOption {
	return func(settings *loadSettings) { settings.reportDirectory = directory }
}

func (settings loadSettings) checkReportLocation(source string) error {
	if settings.reportDirectory == "" {
		return nil
	}
	sourceDir, err := filepath.Abs(filepath.Dir(source))
	if err != nil {
		return fmt.Errorf("resolve case directory: %w", err)
	}
	root, err := filepath.Abs(settings.reportDirectory)
	if err != nil {
		return fmt.Errorf("resolve report directory: %w", err)
	}
	if withinDirectory(sourceDir, root) {
		return errors.New("report directory must be outside the case or suite directory tree")
	}
	// A suite can reference a file through a symlink or Windows junction.
	// Resolve existing portions even when the report directory is new.
	realSource := resolveReportPath(source)
	if withinDirectory(filepath.Dir(realSource), resolveReportPath(root)) {
		return errors.New("report directory must be outside the case or suite directory tree")
	}
	return nil
}

func withinDirectory(directory, path string) bool {
	relative, err := filepath.Rel(directory, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func resolveReportPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	for prefix := full; ; prefix = filepath.Dir(prefix) {
		if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
			if tail, err := filepath.Rel(prefix, full); err == nil {
				return filepath.Join(resolved, tail)
			}
			return full
		}
		if parent := filepath.Dir(prefix); parent == prefix {
			return full
		}
	}
}

type sessionReport struct {
	root, path, secret string
	view               reportView
	published          bool
	failed             bool
}

type reportView struct {
	Title, Source, DeviceID, StartedAt, Diagnostic string
	Suite                                          bool
	State                                          ExecutionState
	Recorded                                       int
	Cases                                          []reportCase
	Counts                                         []reportCount
}

type reportCount struct {
	Status, Label string
	Count         int
}

type reportCase struct {
	Index                                        int
	Name, Source, StartedAt, Diagnostic, IMENote string
	Result                                       Result
	State                                        ExecutionState
	Status                                       string
	Steps                                        []reportStep
}

type reportStep struct {
	Index                   int
	Name, Diagnostic, Image string
	Result                  Result
	Trajectory              []reportTurn
}

type reportTurn struct {
	Index               int
	Action, Note, Image string
	Params              []reportParam
}

type reportParam struct {
	Name, Value string
}

func newSessionReport(settings runSettings, cases []Case, suite bool) *sessionReport {
	if settings.reportDirectory == "" {
		return nil
	}
	report := &sessionReport{root: settings.reportDirectory, secret: settings.model.APIKey}
	view := &report.view
	view.Suite = suite
	input := ""
	if settings.reportInput != "" {
		input, _ = filepath.Abs(settings.reportInput)
		view.Source = report.text(filepath.Base(settings.reportInput))
	}
	view.Title = "套件执行报告"
	if suite && view.Source != "" {
		view.Title = strings.TrimSuffix(view.Source, filepath.Ext(view.Source))
	}
	for i, testCase := range cases {
		name := report.text(testCase.Name)
		source := ""
		if i < len(settings.reportSources) && settings.reportSources[i].Path != "" {
			path := settings.reportSources[i].Path
			source = filepath.Base(path)
			if absolute, err := filepath.Abs(path); input != "" && err == nil {
				if relative, err := filepath.Rel(filepath.Dir(input), absolute); err == nil {
					source = filepath.ToSlash(relative)
				}
			}
			source = report.text(source)
		}
		if name == "" {
			name = fmt.Sprintf("用例 %d", i+1)
		}
		view.Cases = append(view.Cases, reportCase{Index: i + 1, Name: name, Source: source, Status: "not-started"})
	}
	if !suite {
		view.Title = view.Cases[0].Name
	}
	return report
}

func (r *sessionReport) text(value string) string {
	return publicRunText(value, r.secret)
}

func (r *sessionReport) appendCase(index int, record *RunRecord) (_ string, err error) {
	first := r.path == ""
	var directory string
	if first {
		root, err := filepath.Abs(r.root)
		if err != nil {
			r.failed = true
			return "", err
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			r.failed = true
			return "", err
		}
		directory, err = os.MkdirTemp(root, "session-")
		if err != nil {
			r.failed = true
			return "", err
		}
		r.path = filepath.Join(directory, "report.html")
		r.view.DeviceID = r.text(record.DeviceID)
		r.view.StartedAt = record.StartedAt.Format(time.RFC3339Nano)
	} else {
		directory = filepath.Dir(r.path)
		if _, err := os.Stat(r.path); err != nil {
			r.failed = true
			return "", err
		}
	}
	attachments := caseAttachments{directory: directory, index: index}
	defer func() {
		if err == nil {
			return
		}
		r.failed = true
		for _, file := range attachments.written {
			_ = os.Remove(file)
		}
		if first {
			_ = os.Remove(filepath.Join(directory, "attachments"))
			_ = os.Remove(directory)
			r.path = ""
		}
	}()

	item, err := r.caseView(r.view.Cases[index-1], record, &attachments)
	if err != nil {
		return "", err
	}
	r.view.Cases[index-1] = item
	r.view.Recorded++
	if err := r.publish(); err != nil {
		return "", err
	}
	return r.path, nil
}

func (r *sessionReport) finish(state ExecutionState, runErr error) error {
	if r.path == "" || r.failed {
		return nil
	}
	r.view.State = state
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		r.view.Diagnostic = r.text(runErr.Error())
	}
	return r.publish()
}

func (r *sessionReport) publish() error {
	if r.published {
		if _, err := os.Stat(r.path); err != nil {
			return err
		}
	}
	totals := make(map[string]int)
	for _, item := range r.view.Cases {
		totals[item.Status]++
	}
	r.view.Counts = r.view.Counts[:0]
	for _, status := range []string{"pass", "fail", "blocked", "error", "executed", "cancelled", "not-started"} {
		if totals[status] > 0 {
			r.view.Counts = append(r.view.Counts, reportCount{status, reportStatusLabel(status), totals[status]})
		}
	}
	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, r.view); err != nil {
		return err
	}
	if err := fileio.Replace(r.path, buf.Bytes()); err != nil {
		return err
	}
	r.published = true
	return nil
}

func (r *sessionReport) caseView(view reportCase, record *RunRecord, files *caseAttachments) (reportCase, error) {
	view.StartedAt = record.StartedAt.Format(time.RFC3339Nano)
	view.Result, view.State, view.IMENote = record.Result, record.State, r.text(record.IMENote)
	view.Status = string(record.Result)
	if view.Status == "" {
		view.Status = string(record.State)
	}
	if record.Error != nil {
		view.Diagnostic = r.text(record.Error.Error())
	}
	for i, step := range record.Steps {
		image, err := files.save(i+1, 0, step.Screenshot)
		if err != nil {
			return reportCase{}, err
		}
		item := reportStep{Index: i + 1, Name: r.text(step.Name), Result: step.Result, Image: image}
		if item.Name == "" {
			item.Name = fmt.Sprintf("步骤 %d", i+1)
		}
		if step.Error != nil {
			item.Diagnostic = r.text(step.Error.Error())
		}
		for j, turn := range step.Trajectory {
			image, err := files.save(i+1, j+1, turn.Screenshot)
			if err != nil {
				return reportCase{}, err
			}
			action, params := reportAction(turn.Action)
			for i := range params {
				params[i].Value = r.text(params[i].Value)
			}
			item.Trajectory = append(item.Trajectory, reportTurn{
				Index: j + 1, Action: action, Params: params, Note: r.text(turn.Note), Image: image,
			})
		}
		view.Steps = append(view.Steps, item)
	}
	return view, nil
}

type caseAttachments struct {
	directory string
	index     int
	written   []string
}

func (a *caseAttachments) save(step, turn int, data []byte) (string, error) {
	extension := ""
	switch http.DetectContentType(data) {
	case "image/png":
		extension = ".png"
	case "image/jpeg":
		extension = ".jpg"
	case "image/webp":
		extension = ".webp"
	case "image/gif":
		extension = ".gif"
	default:
		return "", nil
	}
	name := fmt.Sprintf("attachments/case-%04d-step-%04d", a.index, step)
	if turn > 0 {
		name += fmt.Sprintf("-turn-%04d", turn)
	}
	name += extension
	path := filepath.Join(a.directory, filepath.FromSlash(name))
	if len(a.written) == 0 {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return "", err
		}
	}
	if err := fileio.Replace(path, data); err != nil {
		return "", err
	}
	a.written = append(a.written, path)
	return name, nil
}

func reportStatusLabel(status string) string {
	switch status {
	case "pass":
		return "通过"
	case "fail":
		return "失败"
	case "blocked":
		return "阻塞"
	case "error":
		return "错误"
	case "executed":
		return "已执行"
	case "finished":
		return "已结束"
	case "cancelled":
		return "已取消"
	case "interrupted":
		return "执行中断"
	case "not-started":
		return "未开始"
	default:
		return "执行检查点"
	}
}

//go:embed report.html.tmpl
var reportHTML string

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"statusLabel": reportStatusLabel,
	"localTime": func(stamp string) string {
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return stamp
		}
		return at.Format("2006-01-02 15:04:05 -07:00")
	},
}).Parse(reportHTML))

func reportAction(step Step) (string, []reportParam) {
	param := func(name string, value any) reportParam {
		return reportParam{Name: name, Value: fmt.Sprint(value)}
	}
	switch {
	case step.Launch != nil:
		params := []reportParam{param("app", step.Launch.App)}
		if step.Launch.Entry != "" {
			params = append(params, param("entry", step.Launch.Entry))
		}
		return "launch", params
	case step.LongPress != nil:
		return "long_press", append(reportLocator(step.LongPress.Locator), param("duration", step.LongPress.Duration))
	case step.TapAt != nil:
		return "tap_at", []reportParam{param("x", step.TapAt.X), param("y", step.TapAt.Y)}
	case step.Swipe != nil:
		s := step.Swipe
		return "swipe", []reportParam{
			param("from_x", s.FromX), param("from_y", s.FromY),
			param("to_x", s.ToX), param("to_y", s.ToY), param("duration", s.Duration),
		}
	case step.Type != nil:
		return "type", []reportParam{param("text", step.Type.Text)}
	case step.Key != nil:
		return "key", []reportParam{param("name", step.Key.Name)}
	case step.Wait != nil:
		return "wait", []reportParam{param("duration", step.Wait.Duration)}
	case step.Locator != (Locator{}):
		return "tap", reportLocator(step.Locator)
	default:
		return "Unknown action", nil
	}
}

func reportLocator(locator Locator) []reportParam {
	fields := []struct {
		name  string
		value *string
	}{
		{"id", locator.ID}, {"text", locator.Text},
		{"description", locator.Description}, {"type", locator.Type},
	}
	var params []reportParam
	for _, field := range fields {
		if field.value != nil {
			params = append(params, reportParam{Name: "locator." + field.name, Value: *field.value})
		}
	}
	if locator.Index != 0 {
		params = append(params, reportParam{Name: "locator.index", Value: strconv.Itoa(locator.Index)})
	}
	return params
}
