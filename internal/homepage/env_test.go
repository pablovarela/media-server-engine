package homepage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestEnv(t *testing.T) {
	type Given struct {
		sources bool
	}
	type Then struct {
		env string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"every source": {
			Given: Given{sources: true},
			Then: Then{env: "HOMEPAGE_VAR_SONARR_KEY=s\nHOMEPAGE_VAR_RADARR_KEY=r\nHOMEPAGE_VAR_PROWLARR_KEY=p\nHOMEPAGE_VAR_DELUGE_PASSWORD=d\n" +
				"HOMEPAGE_VAR_JELLYFIN_KEY=j\nHOMEPAGE_VAR_SEERR_KEY=se\nHOMEPAGE_VAR_BAZARR_KEY=b\nHOMEPAGE_VAR_GLUETUN_KEY=g\nHOMEPAGE_VAR_HEALTHCHECKS_KEY=h\n"},
		},
		"no sources": {
			Then: Then{env: "HOMEPAGE_VAR_SONARR_KEY=\nHOMEPAGE_VAR_RADARR_KEY=\nHOMEPAGE_VAR_PROWLARR_KEY=\nHOMEPAGE_VAR_DELUGE_PASSWORD=\n" +
				"HOMEPAGE_VAR_JELLYFIN_KEY=\nHOMEPAGE_VAR_SEERR_KEY=\nHOMEPAGE_VAR_BAZARR_KEY=\nHOMEPAGE_VAR_GLUETUN_KEY=\nHOMEPAGE_VAR_HEALTHCHECKS_KEY=\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			i := fixture(t, "main")
			if tt.Given.sources {
				write(t, filepath.Join(i.State, ".secrets", "apps.env"), "SONARR_API_KEY=s\nRADARR_API_KEY=r\nPROWLARR_API_KEY=p\nDELUGE_WEB_PASSWORD=d\n")
				write(t, filepath.Join(i.State, ".secrets", "healthchecks.env"), "HEALTHCHECKS_API_KEY=h\n")
				write(t, filepath.Join(i.Data, "volumes", ".wiring", "jellyfin.key"), "j\n")
				write(t, filepath.Join(i.Data, "volumes", ".wiring", "gluetun-control.key"), "g\n")
				write(t, filepath.Join(i.Data, "volumes", "seerr", "config", "settings.json"), `{"main":{"apiKey":"se"}}`)
				write(t, filepath.Join(i.Data, "volumes", "bazarr", "config", "config", "config.yaml"), "auth:\n  apikey: b\n")
			}

			assert.Equal(t, tt.Then.env, Env(i))
		})
	}
}

func TestWriteEnv(t *testing.T) {
	i := fixture(t, "main")

	changed, err := WriteEnv(i, "HOMEPAGE_VAR_X=1\n")
	require.NoError(t, err)
	assert.True(t, changed)
	info, err := os.Stat(filepath.Join(i.State, ".secrets", "homepage.env"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	changed, err = WriteEnv(i, "HOMEPAGE_VAR_X=1\n")
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestWriteEnvTightensAnExistingFile(t *testing.T) {
	i := fixture(t, "main")
	path := filepath.Join(i.State, ".secrets", "homepage.env")
	write(t, path, "HOMEPAGE_VAR_X=1\n")
	require.NoError(t, os.Chmod(path, 0o644))

	_, err := WriteEnv(i, "HOMEPAGE_VAR_X=1\n")

	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
