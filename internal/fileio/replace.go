package fileio

import (
	"os"
	"path/filepath"
)

// Replace publishes a complete file after writing and flushing it in the same directory.
func Replace(path string, data []byte) error {
	return publish(path, data, os.Rename)
}

// Create publishes an already-flushed new file without replacing an existing destination.
func Create(path string, data []byte) error {
	return publish(path, data, os.Link)
}

func publish(path string, data []byte, finish func(string, string) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".gnosis-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return finish(file.Name(), path)
}
