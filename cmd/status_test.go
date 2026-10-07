package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/version"
)

const (
	recentSnapshot = `[{"time":"2026-10-05T03:30:00Z","tags":["machine:this-machine","machine-name:gorgon"]}]`
	laptopSnapshot = `[{"time":"2026-10-05T03:30:00Z","tags":["machine:other","machine-name:laptop"]}]`
)

func succeededTimers(units []string) string {
	var blocks []string
	for _, unit := range units {
		if strings.HasSuffix(unit, ".service") {
			blocks = append(blocks, "ActiveState=inactive\nResult=success\nId="+unit+"\nLoadState=loaded")
		} else if strings.HasSuffix(unit, ".timer") {
			blocks = append(blocks, "LastTriggerUSec=Mon 2026-10-05 03:30:58 UTC\nNextElapseUSecRealtime=Tue 2026-10-06 03:30:00 UTC\nResult=success\nId="+unit+"\nLoadState=loaded")
		}
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

type statusGiven struct {
	running   []string
	snapshots process.Result
	systemd   bool
	apps      string
	settings  string
	dockerErr error
	records   map[string]string
}

func statusDependencies(t *testing.T, given statusGiven) (string, Dependencies, *[]string) {
	t.Helper()
	home, _, _, tmp := backupHome(t)
	config := filepath.Join(home, ".config", "mse", "gorgon")
	if given.settings != "" {
		current, err := os.ReadFile(filepath.Join(config, "installation.env"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(config, "installation.env"), append(current, given.settings...), 0o644))
	}
	for job, record := range given.records {
		dir := filepath.Join(home, ".local", "state", "mse", "gorgon", "runs")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, job+".json"), []byte(record), 0o644))
	}
	if given.apps != "" {
		require.NoError(t, os.WriteFile(filepath.Join(config, "secrets", "apps.sops.env"), []byte(given.apps), 0o644))
	}
	var pings []string
	deps := backupDependencies(t, home, tmp, statusRunner(t, given), statusComposer(t, given), false, &pings)
	deps.Build = version.Build{Version: "v0.23.0"}
	deps.Systemd = func() bool { return given.systemd }
	if given.dockerErr != nil {
		deps.Compose = func(io.Writer, *compose.Outcomes) (composeRunner, error) { return nil, given.dockerErr }
	}
	return home, deps, &pings
}

func statusRunner(t *testing.T, given statusGiven) commandRunner {
	runner := newMockCommandRunner(t)
	runner.EXPECT().Output(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, c process.Command) (process.Result, error) {
		switch c.Name {
		case "restic":
			return given.snapshots, nil
		case "systemctl":
			require.True(t, given.systemd, "systemctl called without systemd")
			return process.Result{Stdout: []byte(succeededTimers(c.Args))}, nil
		}
		t.Fatalf("unexpected command %s %v", c.Name, c.Args)
		return process.Result{}, nil
	}).Maybe()
	return runner
}

func statusComposer(t *testing.T, given statusGiven) composeRunner {
	composer := newMockComposeRunner(t)
	if given.dockerErr == nil {
		containers := []compose.Container{{Name: "jellyfin", State: "running"}, {Name: "sonarr", State: "exited"}}
		for n := range given.running {
			containers[n].State = "running"
		}
		composer.EXPECT().Containers(mock.Anything, "media-server").Return(containers, nil)
	}
	return composer
}

func TestStatus(t *testing.T) {
	type Given = statusGiven
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
			Then:  Then{code: 1, contains: []string{"Couldn't reach the backup repository", "Stack: 2 of 2 services running", "Apps:", "the backup repository couldn't be read"}},
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
		"docker unreachable": {
			Given: Given{snapshots: healthy.snapshots, systemd: true, dockerErr: errors.New("Cannot connect to the Docker daemon")},
			Then:  Then{code: 1, contains: []string{"Stack: couldn't read it: Cannot connect to the Docker daemon", "This machine is the main", "check-backup", "Jellyfin"}},
		},
		"a recorded failure": {
			Given: Given{running: healthy.running, snapshots: healthy.snapshots, systemd: true,
				records: map[string]string{"update": `{"started":"2026-10-05T04:00:58Z","pid":1,"ended":"2026-10-05T04:03:00Z","failed":true}`,
					"backup": `{"started":"2026-10-05T03:30:58Z","pid":1,"ended":"2026-10-05T03:31:41Z"}`}},
			Then: Then{code: 1, contains: []string{"last ran 5 Oct 04:00, failed", "last ran 5 Oct 03:30, succeeded", "update's last run failed"}},
		},
		"a corrupt record leaves the other timers": {
			Given: Given{running: healthy.running, snapshots: healthy.snapshots, systemd: true,
				records: map[string]string{"update": "", "backup": `{"started":"2026-10-05T03:30:58Z","pid":1,"ended":"2026-10-05T03:31:41Z"}`}},
			Then: Then{code: 1, contains: []string{"couldn't read its last run", "last ran 5 Oct 03:30, succeeded", "update's last run couldn't be read"}},
		},
		"no passwords": {
			Given: Given{running: healthy.running, snapshots: healthy.snapshots, systemd: true, apps: "JELLYFIN_ADMIN_PASSWORD=s3cret\n"},
			Then:  Then{absent: []string{"s3cret"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home, deps, pings := statusDependencies(t, tt.Given)
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
			assert.Empty(t, *pings)
			assert.NoDirExists(t, filepath.Join(home, ".local", "state", "mse", "gorgon", ".secrets"), "status writes no decrypted secrets")
		})
	}
}

func TestRemovedCommands(t *testing.T) {
	for _, command := range []string{"urls", "backup-role", "claim-backup-main", "unlock-backup", "monitoring", "homepage", "prune-stack-images", "verify-backup", "remove-executable-downloads"} {
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

func TestNoCommandTakesAnInstallationFlag(t *testing.T) {
	for _, args := range [][]string{{"logins", "--installation", "gorgon"}, {"apply", "--after-update=a1b2c3", "--installation", "gorgon"}} {
		t.Run(args[0], func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t)})
			var stderr bytes.Buffer
			root.SetErr(&stderr)

			code := run(context.Background(), root, args)

			assert.Equal(t, 1, code)
			assert.Contains(t, stderr.String(), "unknown flag: --installation")
		})
	}
}
