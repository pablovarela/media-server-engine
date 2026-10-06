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

func TestURLs(t *testing.T) {
	type Given struct {
		env string
	}
	type When struct {
		args []string
	}
	type Then struct {
		code   int
		stdout string
		stderr string
	}
	host := installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"every app with the landing page": {
			Given: Given{env: "INSTALLATION_NAME=gorgon\n"},
			When:  When{args: []string{"urls"}},
			Then: Then{stdout: "Home         http://gorgon.local\n" +
				"Jellyfin     http://gorgon.local:8096\nSeerr        http://gorgon.local:5055\nSonarr       http://gorgon.local:8989\n" +
				"Radarr       http://gorgon.local:7878\nProwlarr     http://gorgon.local:9696\nBazarr       http://gorgon.local:6767\n" +
				"Deluge       http://gorgon.local:8112\nMaintainerr  http://gorgon.local:6246\nPortainer    http://gorgon.local:9000\n"},
		},
		"landing page on another port": {
			Given: Given{env: "INSTALLATION_NAME=gorgon\nHOMEPAGE_PORT=8080\n"},
			When:  When{args: []string{"urls"}},
			Then:  Then{stdout: "Home         http://gorgon.local:8080\n"},
		},
		"no installation": {
			When: When{args: []string{"urls", "--installation", "nope"}},
			Then: Then{code: 1, stderr: "mse: no installation nope in <home>/.config/mse\n  gorgon\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := tt.Given.env
			if env == "" {
				env = "INSTALLATION_NAME=gorgon\n"
			}
			getenv, home := xdgHome(t, map[string]string{"gorgon": env})
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Host: host, Update: newMockUpdater(t)})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, tt.Then.code, code)
			if tt.Then.code == 0 {
				assert.Contains(t, stdout.String(), tt.Then.stdout)
			}
			assert.Equal(t, replaceHome(tt.Then.stderr, home), stderr.String())
		})
	}
}

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
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Host: host, Update: newMockUpdater(t), Build: version.Build{Version: tt.version}})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{"urls"})

			assert.Equal(t, tt.code, code)
			assert.Equal(t, tt.stderr, stderr.String())
		})
	}
}
