package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

func decrypting(files map[string]string) Decrypter {
	return func(path string) ([]byte, error) {
		text, ok := files[filepath.Base(path)]
		if !ok {
			return nil, errors.New("unexpected decrypt of " + path)
		}
		return []byte(text), nil
	}
}

func fixedKey() (string, error) { return "0123456789abcdef0123456789abcdef", nil }

func newInstallation(t *testing.T, healthchecks bool, configarr string) *installation.Installation {
	t.Helper()
	root := t.TempDir()
	i := &installation.Installation{Name: "gorgon", Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state")}
	require.NoError(t, os.MkdirAll(filepath.Join(i.Config, "secrets"), 0o755))
	for _, name := range []string{"vpn.sops.env", "apps.sops.env"} {
		require.NoError(t, os.WriteFile(filepath.Join(i.Config, "secrets", name), []byte("encrypted"), 0o644))
	}
	if healthchecks {
		require.NoError(t, os.WriteFile(filepath.Join(i.Config, "secrets", "healthchecks.sops.env"), []byte("encrypted"), 0o644))
	}
	if configarr != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(i.Config, "configarr"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(i.Config, "configarr", "config.yml"), []byte(configarr), 0o644))
	}
	return i
}

func TestWriteAll(t *testing.T) {
	apps := "SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nPORTAINER_ADMIN_PASSWORD=port\"pw\nJELLYFIN_ADMIN_PASSWORD=jf\n"
	type Given struct {
		healthchecks bool
		configarr    string
	}
	type Then struct {
		files map[string]string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"every secret file": {
			Given: Given{healthchecks: true, configarr: "sonarr:\n  api_key: !secret SONARR_API_KEY\nradarr:\n  api_key: !secret RADARR_API_KEY\n"},
			Then: Then{files: map[string]string{
				"vpn.env":               "WIREGUARD_PRIVATE_KEY=wg\n",
				"apps.env":              apps,
				"healthchecks.env":      "HEALTHCHECKS_API_KEY=hc\n",
				"gluetun.env":           `HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"0123456789abcdef0123456789abcdef"}` + "\n",
				"sonarr.env":            "SONARR__AUTH__APIKEY=s1\n",
				"radarr.env":            "RADARR__AUTH__APIKEY=r1\n",
				"prowlarr.env":          "PROWLARR__AUTH__APIKEY=p1\n",
				"portainer_admin":       `port"pw`,
				"configarr/secrets.yml": "RADARR_API_KEY: \"r1\"\nSONARR_API_KEY: \"s1\"\n",
			}},
		},
		"no healthchecks secrets and no configarr config": {
			Then: Then{files: map[string]string{
				"healthchecks.env":      "",
				"configarr/secrets.yml": "",
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			i := newInstallation(t, tt.Given.healthchecks, tt.Given.configarr)
			files := map[string]string{"vpn.sops.env": "WIREGUARD_PRIVATE_KEY=wg\n", "apps.sops.env": apps, "healthchecks.sops.env": "HEALTHCHECKS_API_KEY=hc\n"}

			require.NoError(t, WriteAll(i, decrypting(files), fixedKey))

			secrets := filepath.Join(i.State, ".secrets")
			info, err := os.Stat(secrets)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
			for file, want := range tt.Then.files {
				got, err := os.ReadFile(filepath.Join(secrets, file))
				require.NoError(t, err, file)
				assert.Equal(t, want, string(got), file)
				info, err := os.Stat(filepath.Join(secrets, file))
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), file)
			}
			key, err := os.ReadFile(filepath.Join(i.Data, "volumes", ".wiring", "gluetun-control.key"))
			require.NoError(t, err)
			assert.Equal(t, "0123456789abcdef0123456789abcdef\n", string(key))
		})
	}
}

func TestWriteAllKeepsTheGluetunKey(t *testing.T) {
	i := newInstallation(t, false, "")
	keyFile := filepath.Join(i.Data, "volumes", ".wiring", "gluetun-control.key")
	require.NoError(t, os.MkdirAll(filepath.Dir(keyFile), 0o700))
	require.NoError(t, os.WriteFile(keyFile, []byte("existing\n"), 0o600))

	require.NoError(t, WriteAll(i, decrypting(map[string]string{"vpn.sops.env": "", "apps.sops.env": ""}), fixedKey))

	gluetun, err := os.ReadFile(filepath.Join(i.State, ".secrets", "gluetun.env"))
	require.NoError(t, err)
	assert.Contains(t, string(gluetun), `"apikey":"existing"`)
}

func TestWriteAllLeavesUnchangedSecretsAlone(t *testing.T) {
	i := newInstallation(t, false, "")
	decrypt := decrypting(map[string]string{"vpn.sops.env": "A=1\n", "apps.sops.env": "B=2\n"})
	require.NoError(t, WriteAll(i, decrypt, fixedKey))
	vpn := filepath.Join(i.State, ".secrets", "vpn.env")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(vpn, past, past))

	require.NoError(t, WriteAll(i, decrypt, fixedKey))

	info, err := os.Stat(vpn)
	require.NoError(t, err)
	assert.WithinDuration(t, past, info.ModTime(), time.Second)
}

func TestWriteAllCreatesHomepageEnvOnlyWhenMissing(t *testing.T) {
	i := newInstallation(t, false, "")
	decrypt := decrypting(map[string]string{"vpn.sops.env": "", "apps.sops.env": ""})
	require.NoError(t, WriteAll(i, decrypt, fixedKey))
	path := filepath.Join(i.State, ".secrets", "homepage.env")
	created, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Empty(t, created)
	require.NoError(t, os.WriteFile(path, []byte("HOMEPAGE_VAR_X=1\n"), 0o600))

	require.NoError(t, WriteAll(i, decrypt, fixedKey))

	kept, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "HOMEPAGE_VAR_X=1\n", string(kept))
}

func TestApps(t *testing.T) {
	i := newInstallation(t, false, "")

	apps, err := Apps(i, decrypting(map[string]string{"apps.sops.env": "DELUGE_WEB_PASSWORD=d\n"}))

	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DELUGE_WEB_PASSWORD": "d"}, apps)
}
