package gnosis

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// WithReports saves an offline HTML report for each case that starts. The
// caller selects a report directory outside the case and suite files' tree.
func WithReports(directory string) RunOption {
	return func(settings *runSettings) { settings.reportDirectory = directory }
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

func saveReport(root string, record *RunRecord, secret string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return "", err
	}
	tempPath := filepath.Join(directory, ".report.tmp")
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tempPath)
			_ = os.Remove(directory)
		}
	}()
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	err = reportTemplate.Execute(file, viewReport(record, secret))
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	path := filepath.Join(directory, "report.html")
	if err := os.Rename(tempPath, path); err != nil {
		return "", err
	}
	published = true
	return path, nil
}

type reportView struct {
	CaseName, DeviceID, StartedAt, Diagnostic, IMENote string
	Result                                             Result
	Steps                                              []reportStep
}

type reportStep struct {
	Name, Diagnostic string
	Result           Result
	Image            template.URL
	Trajectory       []reportTurn
}

type reportTurn struct {
	Action, Note string
	Params       []reportParam
	Image        template.URL
}

type reportParam struct {
	Name, Value string
}

func viewReport(record *RunRecord, secret string) reportView {
	text := func(value string) string {
		if secret == "" {
			return value
		}
		return strings.ReplaceAll(value, secret, "[redacted]")
	}
	view := reportView{
		CaseName: text(record.CaseName), DeviceID: text(record.DeviceID),
		StartedAt: record.StartedAt.Format(time.RFC3339Nano), Result: record.Result,
		IMENote: text(record.IMENote),
	}
	if record.Error != nil {
		view.Diagnostic = text(record.Error.Error())
	}
	for i, step := range record.Steps {
		item := reportStep{Name: text(step.Name), Result: step.Result, Image: reportImage(step.Screenshot)}
		if item.Name == "" {
			item.Name = strconv.Itoa(i + 1)
		}
		if step.Error != nil {
			item.Diagnostic = text(step.Error.Error())
		}
		for _, turn := range step.Trajectory {
			action, params := reportAction(turn.Action)
			for i := range params {
				params[i].Value = text(params[i].Value)
			}
			item.Trajectory = append(item.Trajectory, reportTurn{
				Action: action, Params: params, Note: text(turn.Note), Image: reportImage(turn.Screenshot),
			})
		}
		view.Steps = append(view.Steps, item)
	}
	return view
}

func reportImage(data []byte) template.URL {
	switch mime := http.DetectContentType(data); mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		// Only a fixed image media type and Base64 bytes can form this URL.
		return template.URL("data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data))
	default:
		return ""
	}
}

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

var reportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.CaseName}} · Gnosis</title>
<style>
:root{color-scheme:light dark;font-family:system-ui,sans-serif}
body{max-width:64rem;margin:2rem auto;padding:0 1rem}
h1,h2,h3{overflow-wrap:anywhere}
dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:.3rem 1rem}
dd{margin:0;overflow-wrap:anywhere}
li{padding:.5rem 0}
img{display:block;max-width:100%;max-height:34rem;object-fit:contain}
.verbatim{white-space:pre-wrap;overflow-wrap:anywhere}
</style>
</head>
<body>
<main>
<h1>{{.CaseName}}</h1>
<dl>
<dt>设备</dt><dd>{{.DeviceID}}</dd>
<dt>开始时间</dt><dd>{{.StartedAt}}</dd>
<dt>结果</dt><dd>{{.Result}}</dd>
{{if .IMENote}}<dt>输入法注明</dt><dd class="verbatim">{{.IMENote}}</dd>{{end}}
{{if .Diagnostic}}<dt>诊断</dt><dd class="verbatim">{{.Diagnostic}}</dd>{{end}}
</dl>
<h2>步骤</h2>
{{if .Steps}}<ol>
{{range .Steps}}<li>
<h3>{{.Name}}</h3>
<p>结果 {{.Result}}</p>
{{if .Diagnostic}}<p class="verbatim">{{.Diagnostic}}</p>{{end}}
{{if .Image}}<img alt="步骤结束截图" src="{{.Image}}">{{else}}<p>截图不可用</p>{{end}}
{{if .Trajectory}}<details open>
<summary>AI 轨迹</summary>
<ol>
{{range .Trajectory}}<li>
<strong>{{.Action}}</strong>
{{if .Params}}<dl>{{range .Params}}<dt>{{.Name}}</dt><dd class="verbatim">{{.Value}}</dd>{{end}}</dl>{{end}}
<p class="verbatim">{{.Note}}</p>
{{if .Image}}<img alt="动作结束截图" src="{{.Image}}">{{else}}<p>截图不可用</p>{{end}}
</li>{{end}}
</ol>
</details>{{end}}
</li>{{end}}
</ol>{{else}}<p>没有步骤记录</p>{{end}}
</main>
</body>
</html>
`))
