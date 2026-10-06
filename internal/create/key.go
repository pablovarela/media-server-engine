package create

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"filippo.io/age"
)

type Undo func() error

type Key struct{ Public, Secret string }

func NewKey() (Key, error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return Key{}, err
	}
	return Key{Public: identity.Recipient().String(), Secret: identity.String()}, nil
}

func KeyFile(getenv func(string) string, configBase string) (string, error) {
	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD"} {
		if getenv(variable) != "" {
			return "", fmt.Errorf("mse create adds the new key to a key file, and %s is set: unset SOPS_AGE_KEY and SOPS_AGE_KEY_CMD (SOPS_AGE_KEY_FILE chooses the file)", variable)
		}
	}
	if path := getenv("SOPS_AGE_KEY_FILE"); path != "" {
		return path, nil
	}
	return filepath.Join(configBase, "sops", "age", "keys.txt"), nil
}

type KeyFileChangedError struct {
	Path, Name string
}

func (e *KeyFileChangedError) Error() string {
	return fmt.Sprintf("the new key stays in %s, which changed while mse create ran; remove its lines (# media server %s) by hand", e.Path, e.Name)
}

func AppendKey(path, name string, key Key, today time.Time) (Undo, error) {
	original, err := os.ReadFile(path) //nolint:gosec // the age key file mse create adds to
	existed := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	added := fmt.Sprintf("# media server %s, created %s\n# public key: %s\n%s\n", name, today.Format(time.DateOnly), key.Public, key.Secret)
	if len(original) > 0 && !bytes.HasSuffix(original, []byte("\n")) {
		added = "\n" + added
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := appendTo(path, added); err != nil {
		return nil, err
	}
	expected := append(bytes.Clone(original), added...)
	return func() error {
		current, err := os.ReadFile(path) //nolint:gosec // the same key file
		if err != nil || !bytes.Equal(current, expected) {
			return &KeyFileChangedError{Path: path, Name: name}
		}
		if !existed {
			return os.Remove(path)
		}
		return os.WriteFile(path, original, 0o600) //nolint:gosec // the same key file
	}, nil
}

func appendTo(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // the age key file
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
