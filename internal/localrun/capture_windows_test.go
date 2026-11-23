package localrun

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gnosis"
)

func TestCaptureRefusesOfflineAndReservedDevice(t *testing.T) {
	conf, path, _ := fixture(t)
	m, err := OpenManager(conf)
	if err != nil {
		t.Fatal(err)
	}
	handler := endpoints(m)
	check := func(serial, reason string) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/capture?device="+serial, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), reason) {
			t.Fatalf("capture: %d %s", response.Code, response.Body)
		}
	}
	check("offline", gnosis.ErrDeviceOffline.Error())
	s, err := m.Start(StartRequest{RequestID: "capture-test", Path: path, DeviceID: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = m.Control(s.ID, "cancel")
		await(t, m, s.ID, func(state Snapshot) bool { return !state.Active() })
	})
	check("fixture", gnosis.ErrRunInProgress.Error())
	if _, err := m.Control(s.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	await(t, m, s.ID, func(state Snapshot) bool { return state.Progress.State == gnosis.ExecutionPaused })
	check("fixture", gnosis.ErrRunInProgress.Error())
	if _, err := m.Control(s.ID, "resume"); err != nil {
		t.Fatal(err)
	}
}
