package localrun

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsCredentialRoundTrip(t *testing.T) {
	name := "Gnosis/Test/" + rand.Text()
	const secret = "fixture-key"
	if err := writeCredential(name, secret); err != nil {
		if errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
			t.Skip("Windows Credential Manager requires a user logon session")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := writeCredential(name, ""); err != nil {
			t.Error(err)
		}
	})
	key, err := readCredential(name)
	if err != nil || key != secret {
		t.Fatal("credential did not round trip")
	}
	if err := writeCredential(name, ""); err != nil {
		t.Fatal(err)
	}
	if key, err := readCredential(name); err != nil || key != "" {
		t.Fatal("credential remained after deletion")
	}
}

func TestSettingsContainNoKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.json")
	if err := WriteJSON(path, ModelSummary{Endpoint: "http://localhost", Name: "local", HasKey: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "Key") {
		t.Fatalf("settings file included credential information: %v", err)
	}
}
