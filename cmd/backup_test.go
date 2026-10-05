package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
)

func resticCall(args ...string) any {
	return mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && slices.Equal(args, c.Args) &&
			slices.Contains(c.Env, "RESTIC_REPOSITORY=b2:bucket") && slices.Contains(c.Env, "RESTIC_PASSWORD=secret")
	})
}

func TestBackupCommands(t *testing.T) {
	type Given struct {
		machineID string
	}
	type When struct {
		args []string
	}
	type Then struct {
		expect func(r *mockCommandRunner)
		stdout string
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"backup-role": {
			Given: Given{machineID: "this-machine\n"},
			When:  When{args: []string{"backup-role"}},
			Then: Then{
				expect: func(r *mockCommandRunner) {
					r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{
						Stdout: []byte(`[{"time":"2026-10-05T04:30:00+01:00","tags":["machine:this-machine","machine-name:gorgon"]}]`),
					}, nil)
				},
				stdout: "gorgon's main is gorgon, last backup 2026-10-05 04:30.\nThis machine is the main.\n",
			},
		},
		"unlock-backup --all": {
			When: When{args: []string{"unlock-backup", "--all"}},
			Then: Then{
				expect: func(r *mockCommandRunner) {
					r.EXPECT().Run(mock.Anything, resticCall("unlock", "--remove-all")).Return(0, nil)
					r.EXPECT().Output(mock.Anything, resticCall("list", "locks", "--no-lock")).Return(process.Result{}, nil)
				},
				stdout: "no locks left on the backup repository\n",
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nRESTIC_REPOSITORY=b2:bucket\n"})
			machineID := filepath.Join(t.TempDir(), "machine-id")
			if tt.Given.machineID != "" {
				require.NoError(t, os.WriteFile(machineID, []byte(tt.Given.machineID), 0o644))
			}
			runner := newMockCommandRunner(t)
			tt.Then.expect(runner)
			deps := Dependencies{
				Environment: getenv, Home: home, Update: newMockUpdater(t),
				Decrypt: func(string) ([]byte, error) {
					return []byte("RESTIC_PASSWORD=secret\n"), nil
				},
				Host:          installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon.local", nil }},
				Engine:        fstest.MapFS{"scripts/backup-excludes.txt": {Data: []byte("logs\n")}},
				Run:           func(_, _ io.Writer) commandRunner { return runner },
				MachineIDFile: machineID,
			}
			root := NewRootCommand(deps)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, 0, code, stderr.String())
			assert.Equal(t, tt.Then.stdout, stdout.String())
			assert.NoFileExists(t, filepath.Join(home, ".local", "share", "mse", "gorgon", ".machine-id"), "only the commands that need it read the machine's identity")
		})
	}
}

func TestBackupCommandsNeedARepository(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{
		Environment: getenv, Home: home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) { return []byte("RESTIC_PASSWORD=secret\n"), nil },
		Host:    installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
		Run:     func(_, _ io.Writer) commandRunner { return newMockCommandRunner(t) },
	})
	var stderr bytes.Buffer
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"unlock-backup"})

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: gorgon has no backup repository: set RESTIC_REPOSITORY in installation.env\n", stderr.String())
}
