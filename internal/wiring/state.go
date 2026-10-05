package wiring

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

type State struct {
	Dir string
}

func (s State) Remembered(name string) string {
	content, err := os.ReadFile(filepath.Join(s.Dir, name)) //nolint:gosec // the wiring's own state
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func (s State) Remember(name, value string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, name), []byte(value+"\n"), 0o600)
}

func Fingerprint(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
