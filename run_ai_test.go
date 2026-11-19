package gnosis_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gnosis"
)

const testAPIKey = "test-model-secret-never-record"

type observedModelRequest struct {
	Goal     string
	Tree     []gnosis.Element
	ImageURL string
}

type localModel struct {
	config   gnosis.ModelConfig
	mu       sync.Mutex
	requests []observedModelRequest
}

func startLocalModel(t *testing.T, reply func(int, http.ResponseWriter, *http.Request)) *localModel {
	t.Helper()
	model := &localModel{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" ||
			r.Header.Get("Content-Type") != "application/json" ||
			r.Header.Get("Authorization") != "Bearer "+testAPIKey {
			t.Error("unexpected request method, address, or headers")
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		var request struct {
			Model    string
			Messages []struct {
				Role    string
				Content json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "vision-fixture" ||
			len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
			t.Errorf("invalid multimodal request: %v", err)
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		var parts []struct {
			Type     string
			Text     string
			ImageURL struct{ URL string } `json:"image_url"`
		}
		err := json.Unmarshal(request.Messages[1].Content, &parts)
		if err != nil || len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
			t.Error("request lacks a text and image observation")
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		var observation struct {
			Goal string
			Tree []gnosis.Element `json:"control_tree"`
		}
		if err := json.Unmarshal([]byte(parts[0].Text), &observation); err != nil {
			t.Errorf("decode observation: %v", err)
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		model.mu.Lock()
		model.requests = append(model.requests, observedModelRequest{observation.Goal, observation.Tree, parts[1].ImageURL.URL})
		round := len(model.requests)
		model.mu.Unlock()
		reply(round, w, r)
	}))
	t.Cleanup(server.Close)
	model.config = gnosis.ModelConfig{Endpoint: server.URL + "/v1/chat/completions", Name: "vision-fixture", APIKey: testAPIKey}
	return model
}

func (m *localModel) observations() []observedModelRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

func writeDecision(w http.ResponseWriter, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{
			"finish_reason": "stop",
			"message":       map[string]any{"role": "assistant", "content": content},
		}},
		"upstream_debug": "raw-response-marker",
	})
}

func scriptedModel(t *testing.T, decisions ...string) *localModel {
	t.Helper()
	return startLocalModel(t, func(round int, w http.ResponseWriter, _ *http.Request) {
		if round > len(decisions) {
			t.Errorf("unexpected model request %d", round)
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		writeDecision(w, decisions[round-1])
	})
}

func fixtureScreenshot(index int) []byte {
	var encoded bytes.Buffer
	// Each successive frame has its own known PNG bytes.
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, index, 1))); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}

type imageDevice struct {
	*testDevice
	failFrame   int
	minOCRFrame int
}

func (d *imageDevice) Screenshot() ([]byte, error) {
	_, err := d.testDevice.Screenshot()
	if d.frames == d.failFrame {
		return nil, errors.New("fixture screenshot failed")
	}
	return fixtureScreenshot(d.frames), err
}

func (d *imageDevice) RecognizeScreenText(data []byte) ([]gnosis.TextRegion, error) {
	if d.frames < d.minOCRFrame || !bytes.Equal(data, fixtureScreenshot(d.frames)) {
		return nil, errors.New("wrong fixture image for OCR")
	}
	return d.testDevice.RecognizeScreenText([]byte{byte(d.frames)})
}

func withAI(goal string, limit int) gnosis.Step {
	return gnosis.Step{Name: goal, AI: &gnosis.AI{Goal: goal, MaxSteps: limit}}
}

func runWithModel(t *testing.T, d *imageDevice, c *testClock, config gnosis.ModelConfig, steps ...gnosis.Step) *gnosis.RunRecord {
	t.Helper()
	record, err := gnosis.Run(gnosis.Case{Name: "AI fixture", Steps: steps}, d, c, gnosis.WithModel(config))
	if err != nil || record == nil {
		t.Fatalf("Run = %v, %v", record, err)
	}
	return record
}

func TestRunAIStopsAfterFirstObservation(t *testing.T) {
	id := "inbox"
	tree := []gnosis.Element{{ID: id, Text: "收件箱", Bounds: image.Rect(1, 2, 3, 4)}}
	device := &imageDevice{testDevice: &testDevice{trees: [][]gnosis.Element{tree}}}
	model := scriptedModel(t, `{"stop":true}`)
	record := runWithModel(t, device, &testClock{}, model.config, withAI("打开收件箱", 4),
		gnosis.Step{Expect: &gnosis.Assertion{Exists: &gnosis.Locator{ID: &id}}})

	wantImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(fixtureScreenshot(1))
	got := model.observations()
	if len(got) != 1 || got[0].Goal != "打开收件箱" || got[0].ImageURL != wantImage ||
		!slices.Equal(got[0].Tree, tree) {
		t.Fatalf("observations = %+v", got)
	}
	if record.Result != gnosis.ResultPass || len(record.Steps) != 2 ||
		record.Steps[0].Result != gnosis.ResultExecuted || len(record.Steps[0].Trajectory) != 0 ||
		!bytes.Equal(record.Steps[0].Screenshot, fixtureScreenshot(1)) || record.Steps[1].Result != gnosis.ResultPass {
		t.Fatalf("run = %+v", record)
	}
	if want := []string{"occupy", "install", "tree 1", "screenshot", "tree 2", "screenshot", "release"}; !slices.Equal(device.calls, want) {
		t.Fatalf("device calls = %v, want %v", device.calls, want)
	}
}

func TestRunOnlyTouchesModelWhenAIIsReached(t *testing.T) {
	model := scriptedModel(t)
	ai := withAI("open", 1)
	for _, test := range []struct {
		name   string
		dev    *testDevice
		steps  []gnosis.Step
		result gnosis.Result
		refuse error
	}{
		{name: "offline", dev: &testDevice{offline: true}, steps: []gnosis.Step{ai}, refuse: gnosis.ErrDeviceOffline},
		{name: "occupied", dev: &testDevice{occupied: true}, steps: []gnosis.Step{ai}, refuse: gnosis.ErrDeviceOccupied},
		{name: "plain case", dev: &testDevice{}, steps: []gnosis.Step{{Wait: &gnosis.Wait{}}}, result: gnosis.ResultExecuted},
		{name: "precondition blocked", dev: &testDevice{}, steps: []gnosis.Step{
			{Precondition: &gnosis.Assertion{Exists: &gnosis.Locator{ID: locatorString("none")}}}, ai,
		}, result: gnosis.ResultBlocked},
		{name: "earlier action failed", dev: &testDevice{fail: "home", fault: errors.New("cannot press")}, steps: []gnosis.Step{
			{Key: &gnosis.Key{Name: "home"}}, ai,
		}, result: gnosis.ResultError},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, err := gnosis.Run(gnosis.Case{Steps: test.steps}, &imageDevice{testDevice: test.dev}, &testClock{}, gnosis.WithModel(model.config))
			if test.refuse != nil {
				if record != nil || !errors.Is(err, test.refuse) {
					t.Fatalf("Run = %v, %v", record, err)
				}
			} else if err != nil || record == nil || record.Result != test.result || len(record.Steps) != 1 {
				t.Fatalf("Run = %+v, %v", record, err)
			}
		})
	}
	if count := len(model.observations()); count != 0 {
		t.Fatalf("sent %d requests for unreached AI steps", count)
	}
}

func TestRunAIErrorBeforeDispatch(t *testing.T) {
	for _, test := range []struct {
		name, wantError string
		missingConfig   bool
		deviceError     string
		reply           func(http.ResponseWriter, *http.Request)
		noRequest       bool
	}{
		{name: "missing settings", wantError: "requires a model", missingConfig: true, noRequest: true},
		{name: "unreadable tree", wantError: "control tree", deviceError: "tree 1", noRequest: true},
		{name: "unreadable screenshot", wantError: "screenshot", deviceError: "screenshot", noRequest: true},
		{name: "unauthorized", wantError: "HTTP 401", reply: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "raw-response-marker "+testAPIKey, http.StatusUnauthorized)
		}},
		{name: "invalid response", wantError: "one completion choice", reply: func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "raw-response-marker "+testAPIKey)
		}},
		{name: "timeout", wantError: "timed out", reply: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := startLocalModel(t, func(_ int, w http.ResponseWriter, r *http.Request) {
				if test.noRequest {
					t.Error("sent a model request before setup or observation succeeded")
					http.Error(w, "", http.StatusBadRequest)
					return
				}
				test.reply(w, r)
			})
			config := model.config
			if test.missingConfig {
				config.APIKey = ""
			}
			if test.name == "timeout" {
				config.Timeout = 150 * time.Millisecond
			}
			d := &imageDevice{testDevice: &testDevice{fail: test.deviceError, fault: errors.New("fixture I/O failure")}}
			record := runWithModel(t, d, &testClock{}, config, withAI("open", 3), gnosis.Step{Key: &gnosis.Key{Name: "home"}})
			if record.Result != gnosis.ResultError || record.Error == nil ||
				!strings.Contains(record.Error.Error(), test.wantError) || len(record.Steps) != 1 ||
				len(record.Steps[0].Trajectory) != 0 || slices.Contains(d.calls, "home") || d.occupied {
				t.Fatalf("record = %+v, calls = %v", record, d.calls)
			}
			if got := fmt.Sprintf("%+v", record); strings.Contains(got, testAPIKey) || strings.Contains(got, "raw-response-marker") ||
				strings.Contains(got, config.Endpoint) {
				t.Error("record leaked credentials, endpoint, or raw response")
			}
			if !test.noRequest && len(model.observations()) != 1 {
				t.Fatal("expected one model request without retry")
			}
		})
	}
}

func TestRunAIBoundedTrajectoryUsesNewObservations(t *testing.T) {
	const first = `{"action":"tap_at","params":{"x":0.5,"y":1},"note":"  点开收件箱。  "}`
	const second = `{"action":"tap_at","params":{"x":1,"y":0},"note":"返回列表。"}`
	for _, test := range []struct {
		name      string
		limit     int
		decisions []string
		turns     int
	}{
		{"one action", 1, []string{first}, 1},
		{"exhausted", 2, []string{first, second}, 2},
		{"voluntary stop", 4, []string{first, second, `{"stop":true}`}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := scriptedModel(t, test.decisions...)
			tree := [][]gnosis.Element{{{Text: "初始"}}, {{Text: "第一次点击后"}}, {{Text: "第二次点击后"}}}
			d := &imageDevice{testDevice: &testDevice{size: image.Pt(8, 6), trees: tree}}
			record := runWithModel(t, d, &testClock{}, model.config, withAI("Find inbox", test.limit))
			if record.Result != gnosis.ResultExecuted || len(record.Steps) != 1 {
				t.Fatalf("record = %+v", record)
			}
			step := record.Steps[0]
			if len(step.Trajectory) != test.turns || !bytes.Equal(step.Screenshot, fixtureScreenshot(test.turns+1)) {
				t.Fatalf("unexpected trajectory or end screenshot (turns %d)", len(step.Trajectory))
			}
			requests := model.observations()
			if len(requests) != len(test.decisions) || d.frames != test.turns+1 {
				t.Fatalf("requests = %d, captures = %d", len(requests), d.frames)
			}
			for i, got := range requests {
				if !slices.Equal(got.Tree, tree[i]) ||
					got.ImageURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(fixtureScreenshot(i+1)) {
					t.Fatalf("request %d did not see the latest interface", i+1)
				}
			}
			notes := []string{"  点开收件箱。  ", "返回列表。"}
			for i, got := range step.Trajectory {
				want := []gnosis.TapAt{{X: 0.5, Y: 1}, {X: 1, Y: 0}}[i]
				action := got.Action.TapAt
				if action == nil || *action != want || got.Note != notes[i] || !bytes.Equal(got.Screenshot, fixtureScreenshot(i+2)) {
					t.Fatalf("unexpected turn %d", i+1)
				}
			}
			if !slices.Contains(d.calls, "tap (4,5)") || slices.Contains(d.calls, "tap (7,0)") != (test.turns == 2) {
				t.Fatalf("unexpected taps: %v", d.calls)
			}
		})
	}
}

func TestRunRejectsInvalidModelActionsBeforeDispatch(t *testing.T) {
	for _, test := range []struct{ name, decision string }{
		{"shell", `{"action":"shell","params":{"command":"anything"},"note":"request"}`},
		{"assertion", `{"action":"expect","params":{"exists":{"id":"target"}},"note":"request"}`},
		{"two actions", `[{"stop":true},{"action":"key","params":{"name":"home"},"note":"go"}]`},
		{"stop with action", `{"stop":true,"action":"key","params":{"name":"home"},"note":"go"}`},
		{"duplicate action", `{"action":"launch","action":"key","params":{"name":"home"},"note":"go"}`},
		{"duplicate parameter", `{"action":"key","params":{"name":"back","name":"home"},"note":"go"}`},
		{"wrong key", `{"action":"key","params":{"name":"volume"},"note":"go"}`},
		{"missing locator", `{"action":"tap","params":{},"note":"go"}`},
		{"locator index only", `{"action":"tap","params":{"locator":{"index":1}},"note":"go"}`},
		{"extra parameter", `{"action":"type","params":{"text":"hi","click":true},"note":"go"}`},
		{"missing note", `{"action":"key","params":{"name":"home"}}`},
		{"long note", `{"action":"key","params":{"name":"home"},"note":"` + strings.Repeat("字", 513) + `"}`},
		{"null parameter", `{"action":"type","params":{"text":null},"note":"go"}`},
		{"quoted ratio", `{"action":"tap_at","params":{"x":"1","y":0},"note":"go"}`},
		{"out of range ratio", `{"action":"tap_at","params":{"x":1.1,"y":0},"note":"go"}`},
		{"out of range swipe", `{"action":"swipe","params":{"from_x":0,"from_y":0,"to_x":0,"to_y":-1},"note":"go"}`},
		{"negative duration", `{"action":"wait","params":{"duration":"-1ms"},"note":"go"}`},
		{"wrong duration type", `{"action":"wait","params":{"duration":1},"note":"go"}`},
		{"credential in text", `{"action":"type","params":{"text":"` + testAPIKey + `"},"note":"go"}`},
		{"credential in note", `{"action":"key","params":{"name":"home"},"note":"secret: ` + testAPIKey + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := scriptedModel(t, test.decision)
			d := &imageDevice{testDevice: &testDevice{size: image.Pt(8, 6)}}
			clock := &testClock{}
			record := runWithModel(t, d, clock, model.config, withAI("act", 4))
			if record.Result != gnosis.ResultError || len(record.Steps) != 1 || record.Error == nil ||
				len(record.Steps[0].Trajectory) != 0 ||
				!slices.Equal(d.calls, []string{"occupy", "install", "tree 1", "screenshot", "release"}) ||
				!clock.now.IsZero() || strings.Contains(record.Error.Error(), testAPIKey) {
				t.Fatalf("invalid decision dispatched or recorded: result %v, calls %v", record.Result, d.calls)
			}
		})
	}
}

func TestRunAIUsesTheSameActionsAsScriptedSteps(t *testing.T) {
	locator := gnosis.Locator{
		ID: locatorString("item"), Text: locatorString("查看"),
		Description: locatorString("详情"), Type: locatorString("button"), Index: 2,
	}
	locatorJSON := `{"id":"item","text":"查看","description":"详情","type":"button","index":2}`
	element := gnosis.Element{ID: "item", Text: "查看", Description: "详情", Type: "button", Bounds: image.Rect(0, 0, 2, 2)}
	second := element
	second.Bounds = image.Rect(3, 5, 7, 9)
	newDevice := func() *imageDevice {
		return &imageDevice{testDevice: &testDevice{
			size: image.Pt(8, 6), trees: [][]gnosis.Element{{element, second}},
			regions: [][]gnosis.TextRegion{{{Text: "像素", Bounds: second.Bounds}}},
		}}
	}

	for _, test := range []struct {
		name, params string
		step         gnosis.Step
	}{
		{"launch", `{"app":"example"}`, gnosis.Step{Launch: &gnosis.Launch{App: "example"}}},
		{"launch", `{"app":"example","entry":".Main"}`, gnosis.Step{Launch: &gnosis.Launch{App: "example", Entry: ".Main"}}},
		{"tap", `{"locator":` + locatorJSON + `}`, gnosis.Step{Locator: locator}},
		{"tap", `{"locator":{"text":"像素"}}`, gnosis.Step{Locator: gnosis.Locator{Text: locatorString("像素")}}},
		{"long_press", `{"locator":` + locatorJSON + `}`, gnosis.Step{LongPress: &gnosis.LongPress{Locator: locator, Duration: 800 * time.Millisecond}}},
		{"long_press", `{"locator":` + locatorJSON + `,"duration":"1s"}`, gnosis.Step{LongPress: &gnosis.LongPress{Locator: locator, Duration: time.Second}}},
		{"tap_at", `{"x":0,"y":1}`, gnosis.Step{TapAt: &gnosis.TapAt{Y: 1}}},
		{"swipe", `{"from_x":1,"from_y":0,"to_x":0.5,"to_y":0.5}`, gnosis.Step{Swipe: &gnosis.Swipe{FromX: 1, ToX: 0.5, ToY: 0.5, Duration: 300 * time.Millisecond}}},
		{"swipe", `{"from_x":1,"from_y":0,"to_x":0.5,"to_y":0.5,"duration":"20ms"}`, gnosis.Step{Swipe: &gnosis.Swipe{FromX: 1, ToX: 0.5, ToY: 0.5, Duration: 20 * time.Millisecond}}},
		{"type", `{"text":"中文\ud83d\ude00"}`, gnosis.Step{Type: &gnosis.Type{Text: "中文😀"}}},
		{"type", `{"text":""}`, gnosis.Step{Type: &gnosis.Type{Text: ""}}},
		{"key", `{"name":"back"}`, gnosis.Step{Key: &gnosis.Key{Name: "back"}}},
		{"key", `{"name":"home"}`, gnosis.Step{Key: &gnosis.Key{Name: "home"}}},
		{"wait", `{"duration":"63ms"}`, gnosis.Step{Wait: &gnosis.Wait{Duration: 63 * time.Millisecond}}},
		{"wait", `{"duration":"0ms"}`, gnosis.Step{Wait: &gnosis.Wait{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := scriptedModel(t, fmt.Sprintf(`{"action":%q,"params":%s,"note":"do it"}`, test.name, test.params))
			aiDevice, aiClock := newDevice(), &testClock{}
			got := runWithModel(t, aiDevice, aiClock, model.config, withAI("interact", 1))
			scriptDevice, scriptClock := newDevice(), &testClock{}
			script, err := gnosis.Run(gnosis.Case{Steps: []gnosis.Step{test.step}}, scriptDevice, scriptClock)
			if err != nil || script.Result != gnosis.ResultExecuted || got.Result != gnosis.ResultExecuted {
				t.Fatalf("script = %+v, AI = %+v, error = %v", script, got, err)
			}
			actionCalls := func(calls []string) []string {
				return slices.DeleteFunc(slices.Clone(calls), func(call string) bool {
					return call == "screenshot" || strings.HasPrefix(call, "tree ")
				})
			}
			if !slices.Equal(actionCalls(aiDevice.calls), actionCalls(scriptDevice.calls)) || aiClock.now != scriptClock.now {
				t.Fatalf("calls or time differ: AI = %v at %v, script = %v at %v",
					aiDevice.calls, aiClock.now, scriptDevice.calls, scriptClock.now)
			}
			if turns := got.Steps[0].Trajectory; len(turns) != 1 || !reflect.DeepEqual(turns[0].Action, test.step) ||
				!bytes.Equal(turns[0].Screenshot, got.Steps[0].Screenshot) {
				t.Fatalf("trajectory does not contain the shared action and its end frame: %+v", turns)
			}
		})
	}
}

func TestRunAIKeepsPartialTrajectoryOnError(t *testing.T) {
	const first = `{"action":"key","params":{"name":"home"},"note":"first"}`
	const second = `{"action":"key","params":{"name":"back"},"note":"second"}`
	for _, test := range []struct {
		name, fail      string
		reply, failShot int
		turns           int
		endFrame        int
	}{
		{name: "later model error", reply: 2, turns: 1, endFrame: 2},
		{name: "later observation error", fail: "tree 2", turns: 1, endFrame: 2},
		{name: "final allowed action fails", fail: "back", turns: 2, endFrame: 3},
		{name: "final action capture fails", failShot: 3, turns: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := startLocalModel(t, func(round int, w http.ResponseWriter, _ *http.Request) {
				if round == test.reply {
					http.Error(w, "raw-response-marker", http.StatusServiceUnavailable)
					return
				}
				writeDecision(w, []string{first, second}[round-1])
			})
			d := &imageDevice{testDevice: &testDevice{fail: test.fail, fault: errors.New("device action failed")}, failFrame: test.failShot}
			record := runWithModel(t, d, &testClock{}, model.config, withAI("act", 2), gnosis.Step{Wait: &gnosis.Wait{}})
			if record.Result != gnosis.ResultError || record.Error == nil || len(record.Steps) != 1 || d.occupied {
				t.Fatalf("run = %+v, occupied = %v", record, d.occupied)
			}
			step := record.Steps[0]
			if len(step.Trajectory) != test.turns || step.Trajectory[0].Note != "first" ||
				!bytes.Equal(step.Trajectory[0].Screenshot, fixtureScreenshot(2)) {
				t.Fatalf("first trajectory entry lost: %+v", step.Trajectory)
			}
			if test.endFrame > 0 && !bytes.Equal(step.Screenshot, fixtureScreenshot(test.endFrame)) {
				t.Fatal("step did not retain its last frame")
			}
			if test.turns == 2 {
				turn := step.Trajectory[1]
				if turn.Action.Key == nil || turn.Action.Key.Name != "back" || turn.Note != "second" ||
					!bytes.Equal(turn.Screenshot, step.Screenshot) {
					t.Fatal("second action was dispatched and should be in the trajectory")
				}
			}
			if test.failShot > 0 && (step.Screenshot != nil || d.frames != test.failShot) {
				t.Fatal("capture was retried or a stale screenshot was recorded")
			}
		})
	}
}

func TestRunAIMixedCaseKeepsIndependentBudgetsAndRestoresIME(t *testing.T) {
	const asset = `name: Mixed case
steps:
  - launch: {app: example}
  - precondition: {exists: {id: entry}}
  - ai: {goal: type something, max_steps: 1}
  - ai: {goal: return home, max_steps: 1}
  - expect: {exists: {id: entry}}
`
	path := filepath.Join(t.TempDir(), "case.yaml")
	if err := os.WriteFile(path, []byte(asset), 0600); err != nil {
		t.Fatal(err)
	}
	testCase, _, err := gnosis.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	model := scriptedModel(t,
		`{"action":"type","params":{"text":"草稿"},"note":"替换焦点内容"}`,
		`{"action":"key","params":{"name":"home"},"note":"回到主页"}`)
	d := &imageDevice{testDevice: &testDevice{
		trees: [][]gnosis.Element{{{ID: "entry"}}}, changeIME: true,
		fail: "restore", fault: errors.New("restore unavailable"),
	}}
	record, err := gnosis.Run(testCase, d, &testClock{}, gnosis.WithModel(model.config))
	if err != nil || record.Result != gnosis.ResultPass || record.IMENote == "" || len(record.Steps) != 5 || d.occupied {
		t.Fatalf("run = %+v, error = %v", record, err)
	}
	if want := []string{"occupy", "install", "launch example ", "screenshot", "tree 1", "screenshot",
		"tree 2", "screenshot", "text 草稿", "screenshot", "tree 3", "screenshot", "home", "screenshot",
		"tree 4", "screenshot", "restore", "release"}; !slices.Equal(d.calls, want) {
		t.Fatalf("calls = %v, want %v", d.calls, want)
	}
	requests := model.observations()
	if len(requests) != 2 || requests[0].Goal != "type something" || requests[1].Goal != "return home" ||
		len(record.Steps[2].Trajectory) != 1 || len(record.Steps[3].Trajectory) != 1 {
		t.Fatal("AI steps did not keep their own goal, budget and trajectory")
	}
	for i, step := range record.Steps {
		if len(step.Screenshot) == 0 {
			t.Fatalf("missing screenshot on step %d", i+1)
		}
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != asset {
		t.Fatal("case file changed during execution")
	}

	d.fail = ""
	again, err := gnosis.Run(gnosis.Case{}, d, &testClock{})
	if err != nil || again == nil || again.Result != gnosis.ResultExecuted {
		t.Fatalf("subsequent execution could not occupy the device: %v, %v", again, err)
	}
}

func TestRunAIRestoresIMEWhenInputFailsAndRedactsTheError(t *testing.T) {
	model := scriptedModel(t, `{"action":"type","params":{"text":"hi"},"note":"type"}`)
	d := &imageDevice{testDevice: &testDevice{changeIME: true, fail: "text hi", fault: errors.New("failed: " + testAPIKey)}}
	record := runWithModel(t, d, &testClock{}, model.config, withAI("type", 1), gnosis.Step{Key: &gnosis.Key{Name: "home"}})
	if record.Result != gnosis.ResultError || record.Error == nil || !strings.Contains(record.Error.Error(), "[redacted]") ||
		strings.Contains(fmt.Sprintf("%+v", record), testAPIKey) || len(record.Steps) != 1 ||
		len(record.Steps[0].Trajectory) != 1 || d.occupied {
		t.Fatalf("unexpected result or leaked secret (result %v)", record.Result)
	}
	if want := []string{"occupy", "install", "tree 1", "screenshot", "text hi", "screenshot", "restore", "release"}; !slices.Equal(d.calls, want) {
		t.Fatalf("calls = %v, want %v", d.calls, want)
	}
}

func TestRunAIDoesNotTreatModelStopAsPassingAnAssertion(t *testing.T) {
	model := scriptedModel(t, `{"stop":true}`)
	d := &imageDevice{testDevice: &testDevice{}}
	record := runWithModel(t, d, &testClock{}, model.config, withAI("do it", 1),
		gnosis.Step{Expect: &gnosis.Assertion{Exists: &gnosis.Locator{ID: locatorString("not on screen")}}},
		gnosis.Step{Key: &gnosis.Key{Name: "home"}})
	if record.Result != gnosis.ResultFail || len(record.Steps) != 2 || record.Steps[0].Result != gnosis.ResultExecuted ||
		record.Steps[1].Result != gnosis.ResultFail || slices.Contains(d.calls, "home") {
		t.Fatalf("run = %+v, device calls = %v", record, d.calls)
	}
}

func TestRunAILocatorObservesAgainAtDispatch(t *testing.T) {
	model := scriptedModel(t, `{"action":"tap","params":{"locator":{"text":"屏幕文字"}},"note":"打开"}`)
	d := &imageDevice{
		minOCRFrame: 2,
		testDevice: &testDevice{regions: [][]gnosis.TextRegion{
			{{Text: "屏幕文字", Bounds: image.Rect(2, 2, 4, 4)}},
		}},
	}
	record := runWithModel(t, d, &testClock{}, model.config, withAI("tap text", 1))
	if record.Result != gnosis.ResultExecuted || len(record.Steps[0].Trajectory) != 1 ||
		!slices.Contains(d.calls, "tap (3,3)") || !bytes.Equal(record.Steps[0].Screenshot, fixtureScreenshot(3)) {
		t.Fatalf("action did not use a fresh locator frame: result = %v, calls = %v", record.Result, d.calls)
	}
}

func TestRunAIRejectsIncompleteOrMultipleModelChoices(t *testing.T) {
	const stop = `{"finish_reason":"stop","message":{"role":"assistant","content":"{\"stop\":true}"}}`
	for _, test := range []struct{ name, body string }{
		{"multiple choices", `{"choices":[` + stop + `,` + stop + `]}`},
		{"truncated choice", `{"choices":[` + strings.Replace(stop, `"stop","message"`, `"length","message"`, 1) + `]}`},
		{"tool call", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"stop\":true}","tool_calls":[{}]}}]}`},
		{"provider error", `{"error":{"message":"raw-response-marker"},"choices":[` + stop + `]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := startLocalModel(t, func(_ int, w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, test.body) })
			d := &imageDevice{testDevice: &testDevice{}}
			record := runWithModel(t, d, &testClock{}, model.config, withAI("act", 1))
			if record.Result != gnosis.ResultError || record.Error == nil || len(record.Steps[0].Trajectory) != 0 ||
				strings.Contains(record.Error.Error(), "raw-response-marker") {
				t.Fatalf("malformed response was accepted or leaked: %v", record.Result)
			}
		})
	}
}

func TestRunAIDoesNotFollowProviderRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("model request reached a different endpoint")
		http.Error(w, "", http.StatusForbidden)
	}))
	t.Cleanup(target.Close)
	model := startLocalModel(t, func(_ int, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	d := &imageDevice{testDevice: &testDevice{}}
	record := runWithModel(t, d, &testClock{}, model.config, withAI("act", 1))
	if record.Result != gnosis.ResultError || record.Error == nil || !strings.Contains(record.Error.Error(), "HTTP 307") {
		t.Fatalf("redirect was not reported as the original response: %+v", record)
	}
}
