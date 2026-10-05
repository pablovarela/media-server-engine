package configure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/pablovarela/media-server-engine/internal/secrets"
)

type Writer struct {
	Config  string
	Encrypt secrets.Encrypter
}

func Files(changes []Change) (plain bool, secretFiles []string) {
	for _, c := range changes {
		switch {
		case c.File == PlainFile:
			plain = true
		case !slices.Contains(secretFiles, c.File):
			secretFiles = append(secretFiles, c.File)
		}
	}
	return plain, secretFiles
}

func (w Writer) WritePlain(texts Texts, changes []Change) error {
	text := RewriteEnv(string(texts.Plain[PlainFile]), updatesTo(PlainFile, changes), true)
	if err := os.WriteFile(filepath.Join(w.Config, PlainFile), []byte(text), 0o644); err != nil { //nolint:gosec // the config is readable like the rest of the checkout
		return fmt.Errorf("could not write %s: %w", PlainFile, err)
	}
	return nil
}

func (w Writer) WriteSecrets(texts Texts, changes []Change) ([]string, error) {
	_, files := Files(changes)
	var written []string
	for _, file := range files {
		path := filepath.Join(w.Config, file)
		plain := RewriteEnv(string(texts.Plain[file]), updatesTo(file, changes), false)
		encrypted, err := w.Encrypt(path, []byte(plain))
		if err != nil {
			return written, fmt.Errorf("could not encrypt %s: %w", file, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a directory of the checkout
			return written, err
		}
		if err := os.WriteFile(path, encrypted, 0o644); err != nil { //nolint:gosec // encrypted, committed to git
			return written, fmt.Errorf("could not write %s: %w", file, err)
		}
		written = append(written, file)
	}
	return written, nil
}

func (w Writer) Undo(texts Texts, changes []Change) error {
	plain, secretFiles := Files(changes)
	if plain {
		secretFiles = append([]string{PlainFile}, secretFiles...)
	}
	var failed []error
	for _, file := range secretFiles {
		path := filepath.Join(w.Config, file)
		raw, existed := texts.Raw[file]
		if existed {
			failed = append(failed, os.WriteFile(path, raw, 0o644)) //nolint:gosec // the bytes the file had
			continue
		}
		if err := os.Remove(path); !errors.Is(err, fs.ErrNotExist) {
			failed = append(failed, err)
		}
	}
	return errors.Join(failed...)
}

func updatesTo(file string, changes []Change) []Update {
	var updates []Update
	for _, c := range changes {
		if c.File == file {
			updates = append(updates, Update{c.Key, c.After})
		}
	}
	return updates
}
