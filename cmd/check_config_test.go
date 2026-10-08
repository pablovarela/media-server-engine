package cmd

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/secrets"
)

var engineDir, _ = filepath.Abs("..")

func checkableConfig(t *testing.T, change func(config string)) string {
	t.Helper()
	config := t.TempDir()
	template, err := fs.Sub(os.DirFS(engineDir), "config-template")
	require.NoError(t, err)
	require.NoError(t, os.CopyFS(config, template))
	write := func(name, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(config, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(config, name), []byte(text), 0o644))
	}
	write("installation.env", "INSTALLATION_NAME=gorgon\nTZ=Europe/London\nJELLYFIN_ADMIN_USER=pablo\nRESTIC_REPOSITORY=/mnt/backup\n")
	write(".sops.yaml", "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: "+testRecipient+"\n")
	encrypt := secrets.SopsEncrypter(t.TempDir(), filepath.Join(config, ".sops.yaml"))
	for name, plain := range map[string]string{
		"secrets/apps.sops.env":   "SONARR_API_KEY=s\nRADARR_API_KEY=r\nPROWLARR_API_KEY=p\nJELLYFIN_ADMIN_PASSWORD=j\nDELUGE_WEB_PASSWORD=d\nPORTAINER_ADMIN_PASSWORD=twelve-chars\n",
		"secrets/backup.sops.env": "RESTIC_PASSWORD=restic\n",
		"secrets/vpn.sops.env":    "VPN_SERVICE_PROVIDER=protonvpn\n",
	} {
		sealed, err := encrypt(filepath.Join(config, name), []byte(plain))
		require.NoError(t, err)
		write(name, string(sealed))
	}
	if change != nil {
		change(config)
	}
	return config
}

func checkConfig(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	root := NewRootCommand(Dependencies{Engine: os.DirFS(engineDir), Home: t.TempDir(), Environment: func(string) string { return "" }})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	code := run(context.Background(), root, append([]string{"check-config"}, args...))
	return code, stdout.String(), stderr.String()
}

func TestCheckConfig(t *testing.T) {
	tests := map[string]struct {
		change  func(config string)
		code    int
		stdout  []string
		problem string
	}{
		"the template, filled as mse setup fills it": {stdout: []string{"Checking the config's schema... config 0.", "Checking the settings... done.", "Checking the secrets...", "Loading the stack... done."}},
		"a schema for another major": {
			change: func(c string) { _ = os.WriteFile(filepath.Join(c, "config.yml"), []byte("config: 7\n"), 0o644) },
			code:   1, problem: "this config is schema 7 and this mse reads 0",
		},
		"a bad time zone": {
			change: func(c string) {
				_ = os.WriteFile(filepath.Join(c, "installation.env"), []byte("INSTALLATION_NAME=gorgon\nTZ=Europe/Madird\nJELLYFIN_ADMIN_USER=pablo\nRESTIC_REPOSITORY=/mnt/backup\n"), 0o644)
			},
			code: 1, problem: "installation.env: TZ: Europe/Madird isn't a time zone",
		},
		"a b2 repository without its keys": {
			change: func(c string) {
				_ = os.WriteFile(filepath.Join(c, "installation.env"), []byte("INSTALLATION_NAME=gorgon\nTZ=Europe/London\nJELLYFIN_ADMIN_USER=pablo\nRESTIC_REPOSITORY=b2:bucket:restic\n"), 0o644)
			},
			code: 1, problem: "secrets/backup.sops.env: B2_ACCOUNT_ID: needed for a b2: repository",
		},
		"no installation name": {
			change: func(c string) {
				_ = os.WriteFile(filepath.Join(c, "installation.env"), []byte("TZ=Europe/London\nJELLYFIN_ADMIN_USER=pablo\nRESTIC_REPOSITORY=/mnt/backup\n"), 0o644)
			},
			code: 1, problem: "installation.env: INSTALLATION_NAME: can't be empty",
		},
		"empty B2 keys for a b2 repository": {
			change: func(c string) {
				_ = os.WriteFile(filepath.Join(c, "installation.env"), []byte("INSTALLATION_NAME=gorgon\nTZ=Europe/London\nJELLYFIN_ADMIN_USER=pablo\nRESTIC_REPOSITORY=b2:bucket:restic\n"), 0o644)
				sealed, _ := os.ReadFile(filepath.Join(c, "secrets", "backup.sops.env"))
				_ = os.WriteFile(filepath.Join(c, "secrets", "backup.sops.env"), append([]byte("B2_ACCOUNT_ID=\nB2_ACCOUNT_KEY=\n"), sealed...), 0o644)
			},
			code: 1, problem: "secrets/backup.sops.env: B2_ACCOUNT_ID: needed for a b2: repository",
		},
		"a value added in plain text to a secrets file": {
			change: func(c string) {
				sealed, _ := os.ReadFile(filepath.Join(c, "secrets", "vpn.sops.env"))
				_ = os.WriteFile(filepath.Join(c, "secrets", "vpn.sops.env"), append([]byte("SERVER_COUNTRIES=Netherlands\n"), sealed...), 0o644)
			},
			code: 1, problem: "secrets/vpn.sops.env: SERVER_COUNTRIES isn't encrypted; set it with mse configure",
		},
		"a secrets file in plain text": {
			change: func(c string) {
				_ = os.WriteFile(filepath.Join(c, "secrets", "vpn.sops.env"), []byte("VPN_SERVICE_PROVIDER=protonvpn\n"), 0o644)
			},
			code: 1, problem: "secrets/vpn.sops.env isn't encrypted by sops",
		},
		"a broken override": {
			change: func(c string) {
				_ = os.WriteFile(filepath.Join(c, "compose.override.yml"), []byte("services: [\n"), 0o644)
			},
			code: 1, problem: "compose.override.yml",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config := checkableConfig(t, tt.change)

			code, stdout, stderr := checkConfig(t, config)

			assert.Equal(t, tt.code, code, stderr)
			for _, line := range tt.stdout {
				assert.Contains(t, stdout, line)
			}
			if tt.problem != "" {
				assert.Contains(t, stdout+stderr, tt.problem)
			}
		})
	}
}

func TestCheckConfigReadsTheCurrentFolderByDefault(t *testing.T) {
	config := checkableConfig(t, nil)
	t.Chdir(config)

	code, _, stderr := checkConfig(t)

	assert.Equal(t, 0, code, stderr)
}
