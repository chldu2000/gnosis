package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gnosis"
)

func TestDraftUsesExecutedSourceAndRequiresNewDestination(t *testing.T) {
	const source = "name: original\nsteps:\n  - launch: {app: start}\n  - ai: {goal: first, max_steps: 1}\n  - precondition: {absent: {id: blocker}}\n  - ai: {goal: second, max_steps: 1}\n  - expect: {absent: {id: failure}}\n"
	host, device, dir, _ := commandFixture(t, map[string]string{"source.yaml": source})
	input := filepath.Join(dir, "source.yaml")
	output := filepath.Join(dir, "saved.yaml")
	device.onLaunch = func(app string) {
		if app == "start" {
			if err := os.WriteFile(input, []byte("name: edited\nsteps: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": `{"action":"launch","params":{"app":"target"},"note":"open target"}`}}}})
	}))
	defer srv.Close()
	t.Setenv("GNOSIS_MODEL_API_KEY", "test-key")
	t.Setenv("GNOSIS_MODEL_ENDPOINT", srv.URL)
	t.Setenv("GNOSIS_MODEL_NAME", "fixture")
	host.input = strings.NewReader(output + "\n")
	status, stdout, stderr := runCommand(host, "--draft", "--device", "selected", input)
	if status != 0 {
		t.Fatal(stderr)
	}
	draft, err := gnosis.ReadCaseDraft(strings.TrimSpace(stdout))
	if err != nil {
		t.Fatal(err)
	}
	c, s, err := gnosis.Load(output)
	if err != nil || s != nil || c.Name != "original" || len(c.Steps) != 5 || c.Steps[2].Precondition == nil || c.Steps[4].Expect == nil || len(draft.Turns) != 2 || draft.Turns[1].Row != 3 || !strings.Contains(stderr, "second") {
		t.Fatalf("wrong draft: %+v, %v", c, err)
	}
	if err := draft.SaveNew(input); err == nil {
		t.Fatal("overwrote existing asset")
	}
	t.Setenv("GNOSIS_MODEL_API_KEY", "")
	t.Setenv("GNOSIS_MODEL_NAME", "")
	t.Setenv("GNOSIS_MODEL_ENDPOINT", "")
	if status, _, stderr := runCommand(host, "--device", "selected", output); status != 0 {
		t.Fatal(stderr)
	}
}

func TestDraftRejectsIneligibleExecutions(t *testing.T) {
	for _, tc := range []struct{ name, body, why string }{
		{"no-ai", "steps: [{expect: {absent: {id: x}}}]", "没有 AI"},
		{"no-expect", "steps: []", "通过"},
		{"suite", "cases: [case.yaml]", "单用例"},
		{"failure", "steps: [{expect: {exists: {id: x}, timeout: 0s}}]", "通过"},
		{"blocked", "steps: [{precondition: {exists: {id: x}, timeout: 0s}}]", "通过"},
		{"error", "steps: [{ai: {goal: missing-model}}]", "通过"},
		{"zero-action", "steps: [{ai: {goal: look}}, {expect: {absent: {id: x}}}]", "没有留下动作"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, _, dir, _ := commandFixture(t, map[string]string{"case.yaml": "steps: []", "trial.yaml": tc.body})
			t.Setenv("GNOSIS_MODEL_API_KEY", "")
			t.Setenv("GNOSIS_MODEL_ENDPOINT", "")
			t.Setenv("GNOSIS_MODEL_NAME", "")
			if tc.name == "zero-action" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"stop\":true}"}}]}`))
				}))
				defer server.Close()
				t.Setenv("GNOSIS_MODEL_API_KEY", "dummy")
				t.Setenv("GNOSIS_MODEL_NAME", "fixture")
				t.Setenv("GNOSIS_MODEL_ENDPOINT", server.URL)
			}
			dest := filepath.Join(dir, "new.yaml")
			host.input = strings.NewReader(dest + "\n")
			status, _, diagnostic := runCommand(host, "--device", "selected", "--draft", filepath.Join(dir, "trial.yaml"))
			if status == 0 || !strings.Contains(diagnostic, tc.why) {
				t.Fatalf("exit=%d %s", status, diagnostic)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("created ineligible draft")
			}
		})
	}
}
