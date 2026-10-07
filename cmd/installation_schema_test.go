package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/version"
)

func TestTheConfigSchemaIsCheckedOnlyAgainstARelease(t *testing.T) {
	host := installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }}
	for name, tt := range map[string]struct {
		version string
		code    int
		stderr  string
	}{
		"a dev build reads any schema":  {version: "dev"},
		"a v0 release refuses schema 1": {version: "v0.16.1", code: 1, stderr: "mse: this config is schema 1 and this mse reads 0: update mse\n"},
		"a v1 release reads schema 1":   {version: "v1.0.0"},
	} {
		t.Run(name, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
			require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "mse", "gorgon", "config.yml"), []byte("config: 1\n"), 0o644))
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Host: host, Update: newMockUpdater(t), Build: version.Build{Version: tt.version}, Decrypt: func(string) ([]byte, error) { return nil, nil }})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{"logins"})

			assert.Equal(t, tt.code, code)
			assert.Equal(t, tt.stderr, stderr.String())
		})
	}
}
