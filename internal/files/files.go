package files

import (
	"bytes"
	"os"
	"path/filepath"
)

func WriteIfChanged(path string, content []byte, perm os.FileMode) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, content) { //nolint:gosec // reads the file it is about to write
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // containers running as other users read these folders
		return false, err
	}
	return true, os.WriteFile(path, content, perm)
}
