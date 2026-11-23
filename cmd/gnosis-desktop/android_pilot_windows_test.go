package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"gnosis"
	"gnosis/internal/assetform"
	"gnosis/internal/localrun"
)

// Opt-in acceptance on a designated Android device. The fixture runs on loopback;
// the model protocol double picks a coordinate from the observed real UI.
func TestAndroidAuthoringPilot(t *testing.T) {
	serial := os.Getenv("GNOSIS_PILOT_SERIAL")
	if serial == "" {
		t.Skip("set GNOSIS_PILOT_SERIAL, GNOSIS_PILOT_ADB, GNOSIS_PILOT_DESKTOP and GNOSIS_PILOT_CLI for hardware acceptance")
	}
	cli, adb := os.Getenv("GNOSIS_PILOT_CLI"), os.Getenv("GNOSIS_PILOT_ADB")
	base := t.TempDir()
	if dir := os.Getenv("GNOSIS_PILOT_OUTPUT"); dir != "" {
		var err error
		base, err = os.MkdirTemp(dir, "pilot-")
		if err != nil {
			t.Fatal(err)
		}
	}
	assets := filepath.Join(base, "cases")
	if err := os.Mkdir(assets, 0700); err != nil {
		t.Fatal(err)
	}
	adbCommand := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, adb, append([]string{"-s", serial}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("adb %s: %s %v", args[0], output, err)
		}
		return strings.TrimSpace(string(output))
	}
	var revision atomic.Int32
	pageTitle := fmt.Sprintf("Gnosis catalog %d", time.Now().UnixNano())
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/state" {
			fmt.Fprint(w, revision.Load())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, strings.Replace(pilotPage, "Gnosis test catalog", pageTitle, 1))
	}))
	defer fixture.Close()
	addr, _ := url.Parse(fixture.URL)
	port := adbCommand("reverse", "tcp:0", "tcp:"+addr.Port())
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatalf("unexpected reverse port %q", port)
	}
	defer adbCommand("reverse", "--remove", "tcp:"+port)
	adbCommand("shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "http://127.0.0.1:"+port+"/")
	client, err := localrun.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Get(t.Context(), "", ""); err != nil {
		host := exec.Command(os.Getenv("GNOSIS_PILOT_DESKTOP"), "--execution-host")
		host.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		host.Env = append(os.Environ(), "LOCALAPPDATA="+base)
		if err := host.Start(); err != nil {
			t.Fatal(err)
		}
		defer host.Process.Release() // The execution host exits when its last client closes.
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := client.Get(t.Context(), "", ""); err == nil {
				break
			} else if time.Now().After(deadline) {
				t.Fatal(err)
			}
			time.Sleep(125 * time.Millisecond)
		}
	}
	observe := func(label string) (*gnosis.InterfaceCapture, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 32*time.Second)
		defer cancel()
		for ctx.Err() == nil {
			cap, err := client.Capture(ctx, serial)
			if err != nil {
				t.Fatalf("read device: %v", err)
			}
			loaded := false
			for _, e := range cap.Elements {
				if e.Text == pageTitle || e.Description == pageTitle {
					loaded = true
					break
				}
			}
			if !loaded {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			for i, e := range cap.Elements {
				if (e.Text == label || e.Description == label) && !e.Bounds.Empty() {
					return cap, i
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
		t.Fatalf("fixture element %q did not appear", label)
		return nil, -1
	}
	const product = "Gnosis Amber"
	const expected = "Gnosis Amber detail"
	selectNode := func(app *desktop, doc *openedDocument, cap *gnosis.InterfaceCapture, index int, action string) {
		t.Helper()
		app.picker = elementPicker{open: true, doc: doc, selected: -1}
		app.picker.display(cap)
		app.picker.search = elementLabel(cap.Elements[index])
		view := ui.NewTester(app.view, 1040, 740)
		e := cap.Elements[index]
		click(t, view, strings.Repeat("  ", min(12, e.Depth))+elementLabel(e))
		for _, candidate := range cap.Candidates(index) {
			l := candidate.Locator
			if (candidate.Count == 1 || elementLabel(e) == "Gnosis notifications off") && ((l.Text != nil && *l.Text == elementLabel(e)) || (l.Description != nil && *l.Description == elementLabel(e))) {
				click(t, view, fmt.Sprintf("%s · %d 个匹配", locatorLabel(l), candidate.Count))
				click(t, view, action)
				return
			}
		}
		t.Fatalf("no unique locator for %s", elementLabel(e))
	}
	runCLI := func(name, input, endpoint string, want int) string {
		t.Helper()
		args := []string{"--device", serial, "--report-dir", filepath.Join(base, "reports")}
		if endpoint != "" {
			args = append(args, "--draft", "--model-endpoint", endpoint, "--model", "pilot-local")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cli, append(args, filepath.Join(assets, name))...)
		key := ""
		if endpoint != "" {
			key = "pilot-token"
		}
		cmd.Env = append(os.Environ(), "GNOSIS_MODEL_API_KEY="+key, "GNOSIS_MODEL_ENDPOINT=", "GNOSIS_MODEL_NAME=")
		cmd.Stdin = strings.NewReader(input)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want {
			t.Fatalf("CLI %s: %v %s", name, err, stderr.String())
		}
		report := strings.TrimSpace(stdout.String())
		if _, err := os.Stat(report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	// Allow the new browser tab's toolbar animation to settle.
	observe(product)
	time.Sleep(650 * time.Millisecond)
	toggleCap, toggleIndex := observe("Gnosis notifications off")
	switchApp := &desktop{width: 1040}
	switchDoc := switchApp.addDocument(assetform.New(false))
	selectNode(switchApp, switchDoc, toggleCap, toggleIndex, "新增点击")
	observe("Gnosis notifications off")
	manualStart := time.Now()
	cap, index := observe(product)
	app := &desktop{width: 1040}
	doc := app.addDocument(assetform.New(false))
	selectNode(app, doc, cap, index, "新增点击")
	// Expose the expected detail while authoring the assertion. The independent
	// manual and assisted reruns both navigate there through a real device tap.
	revision.Store(-2)
	detail, idx := observe(expected)
	selectNode(app, doc, detail, idx, "新增断言")
	if err := doc.Save(filepath.Join(assets, "manual.yaml")); err != nil {
		t.Fatal(err)
	}
	revision.Store(1)
	observe(product)
	runCLI("manual.yaml", "", "", 0)
	manualTime := time.Since(manualStart)

	revision.Add(1)
	observe(product)
	assistedStart := time.Now()
	source := filepath.Join(assets, "trial.yaml")
	assert := fmt.Sprintf("text: %q", expected)
	if detail.Elements[idx].Text != expected {
		assert = fmt.Sprintf("description: %q", expected)
	}
	contents := "name: Amber detail\nsteps:\n  - ai: {goal: Open Gnosis Amber, max_steps: 1}\n  - expect: {exists: {" + assert + "}, timeout: 4s}\n"
	if err := os.WriteFile(source, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content json.RawMessage }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) != 2 {
			http.Error(w, "messages", 400)
			return
		}
		var parts []struct {
			Text  string
			Image struct{ URL string } `json:"image_url"`
		}
		if err := json.Unmarshal(req.Messages[1].Content, &parts); err != nil || len(parts) != 2 {
			http.Error(w, "parts", 400)
			return
		}
		var obs struct {
			Tree []gnosis.Element `json:"control_tree"`
		}
		if err := json.Unmarshal([]byte(parts[0].Text), &obs); err != nil {
			http.Error(w, "tree", 400)
			return
		}
		_, b64, _ := strings.Cut(parts[1].Image.URL, ",")
		cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(b64)))
		if err != nil {
			http.Error(w, "image", 400)
			return
		}
		for _, e := range obs.Tree {
			if e.Text == product || e.Description == product {
				decision := fmt.Sprintf(`{"action":"tap_at","params":{"x":%f,"y":%f},"note":"Open the Amber detail"}`, float64(e.Bounds.Min.X+e.Bounds.Dx()/2)/float64(cfg.Width), float64(e.Bounds.Min.Y+e.Bounds.Dy()/2)/float64(cfg.Height))
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": decision}}}})
				return
			}
		}
		http.Error(w, "missing product", 400)
	}))
	defer model.Close()
	report := runCLI("trial.yaml", "\n", model.URL, 0)
	// No device client is available to this workbench during offline review.
	offline := &desktop{width: 1040}
	offline.openCaseDraft(report)
	if offline.doc == nil {
		t.Fatal(offline.problem)
	}
	view := ui.NewTester(offline.view, 1040, 740)
	click(t, view, "查看动作后截图与断言")
	if !offline.evidenceOpen || offline.evidence == nil || !view.HasText("原用例预期断言") {
		t.Fatal("offline review lacks post-action evidence")
	}
	click(t, view, "关闭")
	click(t, view, "复核坐标 · 查看动作前界面")
	if offline.picker.capture == nil {
		t.Fatalf("missing offline capture: %s", offline.problem)
	}
	offline.picker.search = product
	view.Frame()
	before := offline.picker.capture
	found := false
	for _, e := range before.Elements {
		if e.Text == product || e.Description == product {
			click(t, view, strings.Repeat("  ", min(12, e.Depth))+elementLabel(e))
			l := gnosis.Locator{Text: &e.Text}
			if e.Text != product {
				l = gnosis.Locator{Description: &e.Description}
			}
			click(t, view, locatorLabel(l)+" · 1 个匹配")
			click(t, view, "填入当前步骤")
			found = true
			break
		}
	}
	if !found {
		t.Fatal("pre-action product missing")
	}
	if err := offline.doc.Save(filepath.Join(assets, "assisted.yaml")); err != nil {
		t.Fatal(err)
	}
	revision.Add(1)
	observe(product)
	firstPass := runCLI("assisted.yaml", "", "", 0)
	assistedTime := time.Since(assistedStart)
	// Revision 4 swaps the product order. Revision 5 preserves the actions but
	// deliberately shows the wrong product detail.
	revision.Store(4)
	movedCap, moved := observe(product)
	if movedCap.Elements[moved].Bounds == cap.Elements[index].Bounds {
		t.Fatal("product did not move")
	}
	positive := runCLI("assisted.yaml", "", "", 0)
	// Reopen the same saved case through the workbench's execution process.
	revision.Store(6)
	observe(product)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	snapshot, err := client.Start(ctx, localrun.StartRequest{RequestID: pageTitle, Path: filepath.Join(assets, "assisted.yaml"), DeviceID: serial})
	for err == nil && snapshot.Active() {
		snapshot, err = client.Watch(ctx, snapshot.ID, snapshot.Progress.Sequence)
		if snapshot.ID == "" && err == nil {
			snapshot, err = client.Get(ctx, "", pageTitle)
		}
	}
	if err != nil || len(snapshot.Progress.Cases) != 1 || snapshot.Progress.Cases[0].Result != gnosis.ResultPass || snapshot.Progress.ReportPath == "" {
		t.Fatalf("workbench rerun: %+v, %v", snapshot.Progress, err)
	}
	revision.Store(5)
	observe(product)
	negative := runCLI("assisted.yaml", "", "", 1)
	body, err := os.ReadFile(negative)
	if err != nil || !regexp.MustCompile(`<li id="case-1-step-2"[^>]*data-result="fail"`).Match(body) {
		t.Fatal("missing failed assertion report")
	}
	t.Logf("automated workbench, real Android device, local model double; manual=%s assisted=%s; trial=%s first=%s moved=%s fail=%s", manualTime, assistedTime, report, firstPass, positive, negative)
}

const pilotPage = `<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><title>Gnosis test catalog</title>
<style>body{font:24px sans-serif;padding:16px}button{display:block;width:100%;height:68px;margin:12px 0}</style><h1>Test catalog</h1><main></main><button role="switch" aria-checked="false" onclick="this.textContent='Gnosis notifications on';this.setAttribute('aria-checked','true')">Gnosis notifications off</button>
<script>
let rev=-1;
async function refresh(){const n=Number(await (await fetch('/state')).text());if(n===rev)return;rev=n;
const main=document.querySelector('main');main.replaceChildren();
if(rev===-2){const h=document.createElement('h2');h.textContent='Gnosis Amber detail';main.append(h);return}
for (const label of (rev===4?['Gnosis Blue','Gnosis Amber']:['Gnosis Amber','Gnosis Blue'])) {
const b=document.createElement('button');b.textContent=label;b.onclick=()=>{const h=document.createElement('h2');h.textContent=(rev===5?'Gnosis Blue':label)+' detail';main.replaceChildren(h)};main.append(b)}}
refresh();setInterval(refresh,200)
</script>`
