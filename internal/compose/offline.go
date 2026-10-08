package compose

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

var secretFile = regexp.MustCompile(`\.secrets/[\w.-]+\.env`)

func LoadOffline(ctx context.Context, engine fs.FS, config string, settings map[string]string) error {
	state, err := os.MkdirTemp("", "mse-check-config.")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(state) }()
	if err := Prepare(engine, state); err != nil {
		return err
	}
	if err := placeholderSecrets(engine, state); err != nil {
		return err
	}
	i := &installation.Installation{Name: settings["INSTALLATION_NAME"], Config: config, Data: filepath.Join(state, "data"), State: state, Settings: settings}
	profiles, err := Profiles(i, Stack, true)
	if err != nil {
		return err
	}
	runner, err := NewRunner(io.Discard, &Outcomes{})
	if err != nil {
		return err
	}
	_, err = runner.Load(ctx, i, Stack, Variables(i, "localhost", 0), profiles)
	return err
}

func placeholderSecrets(engine fs.FS, state string) error {
	text, err := fs.ReadFile(engine, Stack.EngineFile)
	if err != nil {
		return err
	}
	for _, name := range secretFile.FindAllString(string(text), -1) {
		path := filepath.Join(state, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return err
		}
	}
	return nil
}
