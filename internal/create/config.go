package create

import (
	"io/fs"
	"os"
	"path/filepath"
)

const sopsRules = "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: "

func WriteConfig(template fs.FS, dir, name, recipient string) (Undo, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil { //nolint:gosec // the XDG config folder
		return nil, err
	}
	if err := os.Mkdir(dir, 0o755); err != nil { //nolint:gosec // the config checkout, readable like any clone
		return nil, err
	}
	undo := func() error { return os.RemoveAll(dir) }
	if err := copyTemplate(template, dir); err != nil {
		return undo, err
	}
	generated := map[string]string{
		"installation.env": "INSTALLATION_NAME=" + name + "\n",
		".sops.yaml":       sopsRules + recipient + "\n",
	}
	for file, text := range generated {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil { //nolint:gosec // committed config
			return undo, err
		}
	}
	return undo, nil
}

func copyTemplate(template fs.FS, dir string) error {
	return fs.WalkDir(template, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755) //nolint:gosec // a folder of the checkout
		}
		content, err := fs.ReadFile(template, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644) //nolint:gosec // committed config
	})
}
