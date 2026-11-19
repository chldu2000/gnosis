package localrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/gofrs/flock"
	"golang.org/x/sys/windows"

	"gnosis"
)

type ModelSummary struct {
	Endpoint string
	Name     string
	HasKey   bool `json:"-"`
}

func ModelSettings() (ModelSummary, error) {
	model, err := ReadModel()
	return ModelSummary{Endpoint: model.Endpoint, Name: model.Name, HasKey: model.APIKey != ""}, err
}

func ReadModel() (gnosis.ModelConfig, error) {
	var model gnosis.ModelConfig
	err := withModelLock(func(path string) error {
		settings, err := readModelFile(path)
		if err != nil {
			return err
		}
		key, err := readModelKey()
		model = gnosis.ModelConfig{Endpoint: settings.Endpoint, Name: settings.Name, APIKey: key}
		return err
	})
	return model, err
}

// SaveModel keeps the previous key when key is nil and deletes it when key
// points to an empty string. Keys never enter the settings file.
func SaveModel(settings ModelSummary, key *string) error {
	return withModelLock(func(path string) error {
		if key == nil {
			return WriteJSON(path, settings)
		}
		previous, err := readModelKey()
		if err != nil {
			return err
		}
		if err := writeModelKey(*key); err != nil {
			return err
		}
		if err := WriteJSON(path, settings); err != nil {
			return errors.Join(err, writeModelKey(previous))
		}
		return nil
	})
}

func withModelLock(fn func(string) error) error {
	dir, err := HostDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(dir, "model.lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()
	return fn(filepath.Join(dir, "model.json"))
}

func readModelFile(path string) (ModelSummary, error) {
	var settings ModelSummary
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	return settings, json.Unmarshal(data, &settings)
}

const modelCredential = "Gnosis/Desktop/ModelAPIKey"

const (
	credentialGeneric      = 1
	credentialLocalMachine = 2
	maxCredentialBytes     = 2560
)

type windowsCredential struct {
	Flags, Type             uint32
	TargetName, Comment     *uint16
	LastWritten             windows.Filetime
	BlobSize                uint32
	Blob                    *byte
	Persist, AttributeCount uint32
	Attributes              uintptr
	TargetAlias, UserName   *uint16
}

var (
	credentialDLL = windows.NewLazySystemDLL("advapi32.dll")
	credRead      = credentialDLL.NewProc("CredReadW")
	credWrite     = credentialDLL.NewProc("CredWriteW")
	credDelete    = credentialDLL.NewProc("CredDeleteW")
	credFree      = credentialDLL.NewProc("CredFree")
)

func readModelKey() (string, error) {
	return readCredential(modelCredential)
}

func readCredential(name string) (string, error) {
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var result *windowsCredential
	ok, _, err := credRead.Call(uintptr(unsafe.Pointer(target)), credentialGeneric, 0, uintptr(unsafe.Pointer(&result)))
	if ok == 0 {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return "", nil
		}
		return "", fmt.Errorf("read Windows model credential: %w", err)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(result)))
	return string(unsafe.Slice(result.Blob, result.BlobSize)), nil
}

func writeModelKey(key string) error {
	return writeCredential(modelCredential, key)
}

func writeCredential(name, key string) error {
	target, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	if key == "" {
		ok, _, err := credDelete.Call(uintptr(unsafe.Pointer(target)), credentialGeneric, 0)
		if ok == 0 && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			return fmt.Errorf("delete Windows model credential: %w", err)
		}
		return nil
	}
	if len(key) > maxCredentialBytes {
		return errors.New("the model key is too long for Windows Credential Manager")
	}
	blob := []byte(key)
	credential := windowsCredential{
		Type: credentialGeneric, TargetName: target, BlobSize: uint32(len(blob)), Blob: &blob[0], Persist: credentialLocalMachine,
	}
	ok, _, err := credWrite.Call(uintptr(unsafe.Pointer(&credential)), 0)
	runtime.KeepAlive(credential)
	if ok == 0 {
		return fmt.Errorf("write Windows model credential: %w", err)
	}
	return nil
}
