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

type Staged struct{ path string }

func (s Staged) Path() string { return s.path }

func (s Staged) Commit(path string) error { return os.Rename(s.path, path) }

func (s Staged) Discard() { _ = os.Remove(s.path) }

func Stage(dir, pattern string, content []byte, perm os.FileMode) (Staged, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return Staged{}, err
	}
	staged := Staged{path: file.Name()}
	_, err = file.Write(content)
	if err == nil {
		err = file.Sync()
	}
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err == nil {
		err = os.Chmod(staged.path, perm)
	}
	if err != nil {
		staged.Discard()
		return Staged{}, err
	}
	return staged, nil
}

func WriteAtomically(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // folders other users' containers may read
		return err
	}
	staged, err := Stage(dir, "."+filepath.Base(path)+"-*", content, perm)
	if err != nil {
		return err
	}
	if err := staged.Commit(path); err != nil {
		staged.Discard()
		return err
	}
	return nil
}
