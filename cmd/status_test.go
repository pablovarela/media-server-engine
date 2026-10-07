package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/version"
)

const (
	recentSnapshot  = `[{"time":"2026-10-05T03:30:00Z","tags":["machine:this-machine","machine-name:gorgon"]}]`
	laptopSnapshot  = `[{"time":"2026-10-05T03:30:00Z","tags":["machine:other","machine-name:laptop"]}]`
	succeededTimers = "Result=success\nExecMainExitTimestamp=Mon 2026-10-05 03:31:41 UTC\nId=%s.service\nLoadState=loaded\n\n" +
		"NextElapseUSecRealtime=Tue 2026-10-06 03:30:00 UTC\nResult=success\nId=%s.timer\nLoadState=loaded\n"
)

func TestStatus(t *testing.T) {
	type Given struct {
		running   []string
		snapshots process.Result
		systemd   bool
		apps      string
		settings  string
	}
	type Then struct {
		code     int
		contains []string
		absent   []string
	}
	healthy := Given{running: []string{"jellyfin", "sonarr"}, snapshots: process.Result{Stdout: []byte(recentSnapshot)}, systemd: true}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"a healthy main": {
			Given: healthy,
			Then: Then{contains: []string{"gorgon on mse v0.23.0", "This machine is the main", "Stack: 2 of 2 services running",
				"Last backup: 5 Oct 03:30 by gorgon", "check-backup", "clean-downloads", "Jellyfin", "http://gorgon.local:8096"}},
		},
		"a service down": {
			Given: Given{running: []string{"jellyfin"}, snapshots: healthy.snapshots, systemd: true},
			Then:  Then{code: 1, contains: []string{"Needs attention:", "1 of 2 services isn't running"}},
		},
		"the backup repository unreachable": {
			Given: Given{running: healthy.running, snapshots: process.Result{Exit: 1, Stderr: []byte("dial tcp: no route to host\n")}, systemd: true},
			Then:  Then{contains: []string{"Couldn't reach the backup repository", "Stack: 2 of 2 services running", "Apps:"}},
		},
		"no systemd": {
			Given: Given{running: healthy.running, snapshots: healthy.snapshots},
			Then:  Then{contains: []string{"No timers on this machine (no systemd)."}},
		},
		"another machine is the main": {
			Given: Given{running: healthy.running, snapshots: process.Result{Stdout: []byte(laptopSnapshot)}, systemd: true},
			Then:  Then{contains: []string{"laptop is the main"}},
		},
		"the landing page on another port": {
			Given: Given{running: healthy.running, snapshots: healthy.snapshots, systemd: true, settings: "HOMEPAGE_PORT=8080\n"},
			Then:  Then{contains: []string{"Home         http://gorgon.local:8080"}},
		},
		"no passwords": {
			Given: Given{running: healthy.running, snapshots: healthy.snapshots, systemd: true, apps: "JELLYFIN_ADMIN_PASSWORD=s3cret\n"},
			Then:  Then{absent: []string{"s3cret"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home, _, _, tmp := backupHome(t)
			if tt.Given.settings != "" {
				env := filepath.Join(home, ".config", "mse", "gorgon", "installation.env")
				current, err := os.ReadFile(env)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(env, append(current, tt.Given.settings...), 0o644))
			}
			if tt.Given.apps != "" {
				require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "mse", "gorgon", "secrets", "apps.sops.env"), []byte(tt.Given.apps), 0o644))
			}
			runner := newMockCommandRunner(t)
			runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
				switch c.Name {
				case "restic":
					return tt.Given.snapshots, nil
				case "systemctl":
					require.True(t, tt.Given.systemd, "systemctl called without systemd")
					unit := strings.TrimSuffix(c.Args[2], ".service")
					return process.Result{Stdout: []byte(strings.ReplaceAll(strings.Replace(succeededTimers, "%s", unit, 1), "%s", unit))}, nil
				}
				t.Fatalf("unexpected command %s %v", c.Name, c.Args)
				return process.Result{}, nil
			}).Maybe()
			composer := newMockComposeRunner(t)
			project := &types.Project{Services: types.Services{"jellyfin": {Name: "jellyfin"}, "sonarr": {Name: "sonarr"}}}
			composer.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(project, nil)
			composer.EXPECT().RunningServices(mock.Anything, project).Return(tt.Given.running, nil)
			var pings []string
			deps := backupDependencies(t, home, tmp, runner, composer, false, &pings)
			deps.Build = version.Build{Version: "v0.23.0"}
			deps.Systemd = func() bool { return tt.Given.systemd }
			root := NewRootCommand(deps)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{"status"})

			assert.Equal(t, tt.Then.code, code, stderr.String())
			for _, text := range tt.Then.contains {
				assert.Contains(t, stdout.String(), text)
			}
			for _, text := range tt.Then.absent {
				assert.NotContains(t, stdout.String()+stderr.String(), text)
			}
			assert.Empty(t, pings)
		})
	}
}

func TestRemovedCommands(t *testing.T) {
	for _, command := range []string{"urls", "backup-role"} {
		t.Run(command, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{command})

			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), "unknown command")
		})
	}
}
