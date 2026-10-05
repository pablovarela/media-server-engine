package installation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBases(t *testing.T) {
	type Given struct {
		env map[string]string
	}
	type Then struct {
		bases Bases
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"defaults under home": {
			Given: Given{env: map[string]string{}},
			Then:  Then{bases: Bases{Config: "/home/u/.config", Data: "/home/u/.local/share", State: "/home/u/.local/state"}},
		},
		"XDG variables win": {
			Given: Given{env: map[string]string{"XDG_CONFIG_HOME": "/c", "XDG_DATA_HOME": "/d", "XDG_STATE_HOME": "/s"}},
			Then:  Then{bases: Bases{Config: "/c", Data: "/d", State: "/s"}},
		},
		"empty means unset": {
			Given: Given{env: map[string]string{"XDG_CONFIG_HOME": "", "XDG_DATA_HOME": "/d"}},
			Then:  Then{bases: Bases{Config: "/home/u/.config", Data: "/d", State: "/home/u/.local/state"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			getenv := func(key string) string { return tt.Given.env[key] }
			assert.Equal(t, tt.Then.bases, BasesFrom(getenv, "/home/u"))
		})
	}
}

func TestLoad(t *testing.T) {
	type Given struct {
		installations map[string]string
		strays        []string
	}
	type When struct {
		requested string
	}
	type Then struct {
		name string
		err  string
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"the only installation": {
			Given: Given{installations: map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"}},
			Then:  Then{name: "gorgon"},
		},
		"the requested one among several": {
			Given: Given{installations: map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n", "trial": "INSTALLATION_NAME=trial\n"}},
			When:  When{requested: "trial"},
			Then:  Then{name: "trial"},
		},
		"none": {
			Then: Then{err: "no installation in <config>/mse"},
		},
		"several and none requested": {
			Given: Given{installations: map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n", "trial": "INSTALLATION_NAME=trial\n"}},
			Then:  Then{err: "several installations in <config>/mse: choose one with --installation <name>\n  gorgon\n  trial"},
		},
		"unknown name": {
			Given: Given{installations: map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"}},
			When:  When{requested: "nope"},
			Then:  Then{err: "no installation nope in <config>/mse\n  gorgon"},
		},
		"folders without installation.env are ignored": {
			Given: Given{installations: map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"}, strays: []string{"old"}},
			Then:  Then{name: "gorgon"},
		},
		"name missing in installation.env": {
			Given: Given{installations: map[string]string{"gorgon": "TZ=Europe/London\n"}},
			Then:  Then{err: "INSTALLATION_NAME is not set in <config>/mse/gorgon/installation.env"},
		},
		"name differs from the directory": {
			Given: Given{installations: map[string]string{"gorgon": "INSTALLATION_NAME=other\n"}},
			Then:  Then{err: "INSTALLATION_NAME is other in <config>/mse/gorgon/installation.env, not gorgon"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			bases := Bases{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state")}
			for dir, env := range tt.Given.installations {
				require.NoError(t, os.MkdirAll(filepath.Join(bases.Config, "mse", dir), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(bases.Config, "mse", dir, "installation.env"), []byte(env), 0o644))
			}
			for _, dir := range tt.Given.strays {
				require.NoError(t, os.MkdirAll(filepath.Join(bases.Config, "mse", dir), 0o755))
			}

			loaded, err := Load(bases, tt.When.requested, func(string) string { return "" })

			if tt.Then.err != "" {
				assert.EqualError(t, err, replaceAll(tt.Then.err, "<config>", bases.Config))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.name, loaded.Name)
			assert.Equal(t, filepath.Join(bases.Config, "mse", tt.Then.name), loaded.Config)
			assert.Equal(t, filepath.Join(bases.Data, "mse", tt.Then.name), loaded.Data)
			assert.Equal(t, filepath.Join(bases.State, "mse", tt.Then.name), loaded.State)
			assert.Equal(t, tt.Then.name, loaded.Settings["INSTALLATION_NAME"])
		})
	}
}

func TestDerivedValues(t *testing.T) {
	type Given struct {
		settings   map[string]string
		backupMain bool
	}
	type Then struct {
		role         string
		port         string
		allowedHosts string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"defaults on a secondary": {
			Given: Given{settings: map[string]string{}},
			Then:  Then{role: "secondary", port: "80", allowedHosts: "gorgon.local,localhost,127.0.0.1"},
		},
		"the backup main with another port and extra hosts": {
			Given: Given{settings: map[string]string{"HOMEPAGE_PORT": "8080", "HOMEPAGE_ALLOWED_HOSTS": "media.tail1234.ts.net, other"}, backupMain: true},
			Then:  Then{role: "main", port: "8080", allowedHosts: "gorgon.local:8080,localhost:8080,127.0.0.1:8080,media.tail1234.ts.net:8080,other:8080"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := t.TempDir()
			if tt.Given.backupMain {
				require.NoError(t, os.WriteFile(filepath.Join(data, ".backup-main"), nil, 0o644))
			}
			loaded := &Installation{Name: "gorgon", Data: data, Settings: tt.Given.settings}

			assert.Equal(t, tt.Then.role, loaded.Role())
			assert.Equal(t, tt.Then.port, loaded.HomepagePort())
			assert.Equal(t, tt.Then.allowedHosts, loaded.HomepageAllowedHosts("gorgon.local"))
		})
	}
}

func replaceAll(s, old, replacement string) string { return strings.ReplaceAll(s, old, replacement) }
