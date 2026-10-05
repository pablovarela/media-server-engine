package homepage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pablovarela/media-server-engine/internal/files"
	"github.com/pablovarela/media-server-engine/internal/installation"
)

func syncImages(i *installation.Installation) (bool, error) {
	source := filepath.Join(i.Config, "homepage", "images")
	target := filepath.Join(i.State, ".homepage-images")
	wanted := map[string]bool{}
	changed := false
	err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && path == source {
			return filepath.SkipDir
		}
		if err != nil || d.IsDir() {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path) //nolint:gosec // reads the installation's own page images
		if err != nil {
			return err
		}
		wanted[relative] = true
		wrote, err := files.WriteIfChanged(filepath.Join(target, relative), content, 0o644)
		changed = changed || wrote
		return err
	})
	if err != nil {
		return false, err
	}
	removed, err := removeUnwanted(target, wanted)
	return changed || removed, err
}

func removeUnwanted(dir string, wanted map[string]bool) (bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // Homepage serves these images
		return false, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	removed := false
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || wanted[filepath.FromSlash(path)] {
			return err
		}
		removed = true
		return root.Remove(path)
	})
	return removed, err
}
