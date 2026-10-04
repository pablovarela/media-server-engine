package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pablovarela/media-server-engine/internal/installation"
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
