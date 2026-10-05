package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func MachineID(systemFile, data string) (string, error) {
	if id := trimmedContent(systemFile); id != "" {
		return id, nil
	}
	generated := filepath.Join(data, ".machine-id")
	if id := trimmedContent(generated); id != "" {
		return id, nil
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random)
	if err := os.MkdirAll(data, 0o755); err != nil { //nolint:gosec // the installation's data directory, read by its containers
		return "", err
	}
	return id, os.WriteFile(generated, []byte(id+"\n"), 0o644) //nolint:gosec // an identifier, not a secret
}

func trimmedContent(path string) string {
	text, _ := os.ReadFile(path) //nolint:gosec // the machine id files the engine names
	return strings.TrimSpace(string(text))
}

func markMain(data string) error {
	marker := filepath.Join(data, ".backup-main")
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // an empty marker file
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(marker, now, now)
}

func removeMarker(data string) error {
	if err := os.Remove(filepath.Join(data, ".backup-main")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
