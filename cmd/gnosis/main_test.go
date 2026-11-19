package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gnosis"
)

type commandClock struct{ now time.Time }

func (c *commandClock) Now() time.Time        { return c.now }
func (c *commandClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

// Only the real operations used by these cases are implemented. Other
// operations reaching the embedded interface would fail the test.
type commandDevice struct {
	gnosis.Device
	serial                     string
	offline, occupied          bool
	onlineChecks, offlineAfter int
	operations                 []string
	onLaunch                   func(string)
}

func (d *commandDevice) ID() string { return d.serial }
func (d *commandDevice) Online() bool {
	d.onlineChecks++
	return !d.offline && (d.offlineAfter == 0 || d.onlineChecks <= d.offlineAfter)
}
func (d *commandDevice) Occupied() bool { return d.occupied }
func (d *commandDevice) TryOccupy() bool {
	d.occupied = true
	return true
}
func (d *commandDevice) Release()                               { d.occupied = false }
func (d *commandDevice) EnsureAutomation() error                { return nil }
func (d *commandDevice) ControlTree() ([]gnosis.Element, error) { return nil, nil }
func (d *commandDevice) Screenshot() ([]byte, error) {
	var output bytes.Buffer
	err := png.Encode(&output, image.NewGray(image.Rect(0, 0, 1, 1)))
	return output.Bytes(), err
}
func (d *commandDevice) Launch(app, _ string) error {
	d.operations = append(d.operations, app)
	if d.onLaunch != nil {
		d.onLaunch(app)
	}
	if app == "broken" {
		return errors.New("test application failed")
	}
	return nil
}

func commandFixture(t *testing.T, files map[string]string) (commandHost, *commandDevice, string, string) {
	t.Helper()
	base := t.TempDir()
	assets, reports := filepath.Join(base, "assets"), filepath.Join(base, "reports")
	if err := os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(assets, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	device := &commandDevice{}
	return commandHost{
		openDevice: func(serial string) (gnosis.Device, error) {
			if serial != "selected" {
				t.Errorf("openDevice(%q) instead of the selected serial", serial)
			}
			device.serial = serial
			return device, nil
		},
		reportDirectory: func() (string, error) { return reports, nil },
		clock:           &commandClock{},
	}, device, assets, reports
}

func runCommand(host commandHost, args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	exit := host.run(args, &out, &diagnostics)
	return exit, out.String(), diagnostics.String()
}

func readPaths(t *testing.T, stdout string, names ...string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if stdout == "" {
		lines = nil
	}
	want := 0
	if len(names) > 0 {
		want = 1
	}
	if len(lines) != want {
		t.Fatalf("stdout has %d paths, want %d: %q", len(lines), want, stdout)
	}
	if want == 0 {
		return lines
	}
	path := lines[0]
	if !filepath.IsAbs(path) {
		t.Fatalf("unexpected line on stdout: %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report %q: %v", path, err)
	}
	remaining := string(body)
	for _, name := range names {
		title := "<h2>" + name + "</h2>"
		i := strings.Index(remaining, title)
		if i < 0 {
			t.Fatalf("report %q is missing the ordered case %q", path, name)
		}
		remaining = remaining[i+len(title):]
	}
	return lines
}

func TestCommandRunsSingleCaseAndReturnsItsExitCode(t *testing.T) {
	for _, test := range []struct {
		name, steps string
		wantExit    int
		diagnostic  string
	}{
		{"executed", "steps: [{wait: {duration: 1ms}}]\n", 0, ""},
		{"pass", "steps: [{expect: {absent: {id: missing}}}]\n", 0, ""},
		{"fail", "steps: [{expect: {exists: {id: missing}, timeout: 0ms}}]\n", 1, "fail"},
		{"blocked", "steps: [{precondition: {exists: {id: missing}, timeout: 0ms}}]\n", 1, "blocked"},
		{"error", "steps: [{launch: {app: broken}}]\n", 1, "test application failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, device, assets, _ := commandFixture(t, map[string]string{"case.yaml": test.steps})
			exit, stdout, stderr := runCommand(host, "--device", "selected", filepath.Join(assets, "case.yaml"))
			if exit != test.wantExit || (test.diagnostic == "" && stderr != "") ||
				(test.diagnostic != "" && !strings.Contains(stderr, test.diagnostic)) || device.serial != "selected" || device.occupied {
				t.Fatalf("exit=%d stdout=%q stderr=%q device=%+v", exit, stdout, stderr, device)
			}
			readPaths(t, stdout, "case")
		})
	}
}

func TestCommandValidatesBeforeOpeningTheDevice(t *testing.T) {
	host, device, assets, reports := commandFixture(t, map[string]string{
		"valid.yaml":   "name: valid\n",
		"empty.yaml":   "cases: []\n",
		"late.yaml":    "cases: [valid.yaml, invalid.yaml]\n",
		"invalid.yaml": "unknown_field: 1\n",
		"mixed.yaml":   "cases: [valid.yaml]\nname: bad\n",
	})
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"no device", []string{filepath.Join(assets, "valid.yaml")}, "explicit device"},
		{"no input", []string{"--device", "selected"}, "one case or suite"},
		{"unknown option", []string{"--bad"}, "flag provided but not defined"},
		{"missing file", []string{"--device", "selected", filepath.Join(assets, "missing.yaml")}, "cannot read file"},
		{"empty suite", []string{"--device", "selected", filepath.Join(assets, "empty.yaml")}, "must not be empty"},
		{"bad later reference", []string{"--device", "selected", filepath.Join(assets, "late.yaml")}, "reference 2"},
		{"mixed file", []string{"--device", "selected", filepath.Join(assets, "mixed.yaml")}, "mixes case and suite"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exit, stdout, stderr := runCommand(host, test.args...)
			if exit == 0 || stdout != "" || !strings.Contains(stderr, test.want) || device.serial != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q, device=%q", exit, stdout, stderr, device.serial)
			}
			if _, err := os.Stat(reports); !os.IsNotExist(err) {
				t.Fatal("refused command created reports")
			}
		})
	}
	exit, stdout, stderr := runCommand(host, "--help")
	if exit != 0 || stdout != "" || !strings.Contains(stderr, "usage: gnosis") || device.serial != "" {
		t.Fatalf("help = %d, %q, %q", exit, stdout, stderr)
	}
}

func TestCommandRejectsReportDirectoriesInsideReferencedCaseTree(t *testing.T) {
	host, device, assets, _ := commandFixture(t, map[string]string{"suite.yaml": "cases: [../other/case.yaml]\n"})
	base := filepath.Dir(assets)
	other := filepath.Join(base, "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "case.yaml"), []byte("name: other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{filepath.Join(assets, "reports"), filepath.Join(other, "reports")} {
		host.reportDirectory = func() (string, error) { return root, nil }
		exit, stdout, stderr := runCommand(host, "--device", "selected", filepath.Join(assets, "suite.yaml"))
		if exit == 0 || stdout != "" || !strings.Contains(stderr, "outside the case or suite directory tree") || device.serial != "" {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
		}
	}
}

func TestCommandPrintsOneSuiteReportAndAccountsForAllResults(t *testing.T) {
	for _, test := range []struct {
		name    string
		refs    string
		reports []string
		exit    int
	}{
		{"complete success", "[pass.yaml, repeat.yaml, repeat.yaml]", []string{"pass", "repeat", "repeat"}, 0},
		{"continues after assertions", "[fail.yaml, blocked.yaml, pass.yaml]", []string{"fail", "blocked", "pass"}, 1},
		{"stops at device error", "[pass.yaml, error.yaml, repeat.yaml]", []string{"pass", "error", "repeat"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, device, assets, _ := commandFixture(t, map[string]string{
				"suite.yaml":   "cases: " + test.refs + "\n",
				"pass.yaml":    "steps: [{expect: {absent: {id: missing}}}]\n",
				"repeat.yaml":  "steps: [{launch: {app: example}}]\n",
				"fail.yaml":    "steps: [{expect: {exists: {id: missing}, timeout: 0ms}}]\n",
				"blocked.yaml": "steps: [{precondition: {exists: {id: missing}, timeout: 0ms}}]\n",
				"error.yaml":   "steps: [{launch: {app: broken}}]\n",
			})
			exit, stdout, stderr := runCommand(host, "--device", "selected", filepath.Join(assets, "suite.yaml"))
			if exit != test.exit || (stderr == "") != (test.exit == 0) || device.occupied {
				t.Fatalf("exit=%d stdout=%q stderr=%q, occupied=%v", exit, stdout, stderr, device.occupied)
			}
			readPaths(t, stdout, test.reports...)
			if test.exit == 0 && !slices.Equal(device.operations, []string{"example", "example"}) {
				t.Fatal("repeated references did not run separately")
			}
			if test.name == "stops at device error" && !slices.Equal(device.operations, []string{"broken"}) {
				t.Fatalf("suite continued after an error: %v", device.operations)
			}
		})
	}
}

func TestCommandReportsRefusalInterruptionAndSaveFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		diagnostic string
		reports    []string
	}{
		{"offline", "device is offline", nil},
		{"interrupted", "could not start", []string{"case"}},
		{"save", "save report", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			host, device, assets, reports := commandFixture(t, map[string]string{
				"suite.yaml": "cases: [case.yaml, case.yaml]\n",
				"case.yaml":  "steps: [{wait: {duration: 0ms}}]\n",
			})
			switch test.name {
			case "offline":
				device.offline = true
			case "interrupted":
				device.offlineAfter = 1
			case "save":
				if err := os.WriteFile(reports, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			exit, stdout, stderr := runCommand(host, "--device", "selected", filepath.Join(assets, "suite.yaml"))
			if exit == 0 || !strings.Contains(stderr, test.diagnostic) || device.occupied {
				t.Fatalf("exit=%d stdout=%q stderr=%q, occupied=%v", exit, stdout, stderr, device.occupied)
			}
			readPaths(t, stdout, test.reports...)
		})
	}
}

func TestCommandPrintsEarlierReportWhenLaterSaveFails(t *testing.T) {
	host, device, assets, reports := commandFixture(t, map[string]string{
		"suite.yaml": "cases: [first.yaml, later.yaml, later.yaml]\n",
		"first.yaml": "steps: [{wait: {duration: 0ms}}]\n",
		"later.yaml": "steps: [{launch: {app: second}}]\n",
	})
	// Make the report directory unavailable only while saving the second case.
	parked := reports + "-parked"
	device.onLaunch = func(string) {
		if err := os.Rename(reports, parked); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(reports, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	exit, stdout, stderr := runCommand(host, "--device", "selected", filepath.Join(assets, "suite.yaml"))
	if exit == 0 || !strings.Contains(stderr, "save report") || !slices.Equal(device.operations, []string{"second"}) {
		t.Fatalf("exit=%d stdout=%q stderr=%q, operations=%v", exit, stdout, stderr, device.operations)
	}
	if err := os.Remove(reports); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parked, reports); err != nil {
		t.Fatal(err)
	}
	readPaths(t, stdout, "first")
}

func TestCommandUsesHostModelAndExplicitOverrides(t *testing.T) {
	const secret = "cli-model-test-secret"
	t.Setenv("GNOSIS_MODEL_API_KEY", secret)
	handler := func(w http.ResponseWriter, req *http.Request) {
		var request struct{ Model string }
		err := json.NewDecoder(req.Body).Decode(&request)
		want := "default-model"
		if req.URL.Path == "/override" {
			want = "selected-model"
		}
		if err != nil || req.Header.Get("Authorization") != "Bearer "+secret || request.Model != want {
			t.Errorf("wrong model setup for %s: %q, %v", req.URL.Path, request.Model, err)
			http.Error(w, secret, http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"stop\":true}"}}]}`)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	t.Setenv("GNOSIS_MODEL_NAME", "default-model")
	t.Setenv("GNOSIS_MODEL_ENDPOINT", server.URL+"/default")
	host, _, assets, _ := commandFixture(t, map[string]string{"ai.yaml": "steps: [{ai: {goal: Look}}]\n"})
	source := filepath.Join(assets, "ai.yaml")
	for _, args := range [][]string{
		{"--device", "selected", source},
		{"--device", "selected", "--model-endpoint", server.URL + "/override", "--model", "selected-model", source},
	} {
		exit, stdout, stderr := runCommand(host, args...)
		if exit != 0 || stderr != "" || strings.Contains(stdout, secret) {
			t.Fatalf("AI command exit=%d, stdout=%q, stderr=%q", exit, stdout, stderr)
		}
		readPaths(t, stdout, "ai")
	}
	host.openDevice = func(string) (gnosis.Device, error) { return nil, errors.New("device diagnostic " + secret) }
	exit, stdout, stderr := runCommand(host, "--device", "selected", source)
	if exit == 0 || stdout != "" || strings.Contains(stderr, secret) || !strings.Contains(stderr, "[redacted]") {
		t.Fatalf("device diagnostic leaked configuration: %d, %q, %q", exit, stdout, stderr)
	}
}
