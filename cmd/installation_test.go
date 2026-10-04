package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func xdgHome(t *testing.T, installations map[string]string) (func(string) string, string) {
	t.Helper()
	home := t.TempDir()
	for name, env := range installations {
		config := filepath.Join(home, ".config", "mse", name)
		require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(config, "installation.env"), []byte(env), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(config, "config.yml"), []byte("config: 0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(config, "images.yml"), []byte("services:\n  homepage:\n    image: h@sha256:x\n"), 0o644))
	}
	return func(key string) string { return "" }, home
}

func replaceHome(s, home string) string { return strings.ReplaceAll(s, "<home>", home) }
