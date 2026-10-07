package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
)

const (
	ourSnapshots   = `[{"time":"2026-10-04T04:30:00Z","tags":["machine:this-machine","machine-name:gorgon"]}]`
	theirSnapshots = `[{"time":"2026-10-04T04:30:00Z","tags":["machine:other","machine-name:pi2"]}]`
)

func backupHome(t *testing.T) (home, data, resolved, tmp string) {
	t.Helper()
	_, home = xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nRESTIC_REPOSITORY=b2:bucket\n"})
	config := filepath.Join(home, ".config", "mse", "gorgon")
	require.NoError(t, os.WriteFile(filepath.Join(config, "secrets", "healthchecks.sops.env"), nil, 0o644))
	data = filepath.Join(home, ".local", "share", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(filepath.Join(data, "volumes", "jellyfin"), 0o755))
	resolved, err := filepath.EvalSymlinks(data)
	require.NoError(t, err)
	return home, data, resolved, t.TempDir()
}

func backupDependencies(t *testing.T, home, tmp string, runner commandRunner, composer composeRunner, interactive bool, pings *[]string) Dependencies {
	t.Helper()
	machineID := filepath.Join(t.TempDir(), "machine-id")
	require.NoError(t, os.WriteFile(machineID, []byte("this-machine\n"), 0o644))
	return Dependencies{
		ResticBinary: localRestic,
		Environment: func(key string) string {
			if key == "TMPDIR" {
				return tmp
			}
			return ""
		},
		Home: home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) {
			return []byte("RESTIC_PASSWORD=secret\nHEALTHCHECKS_PING_KEY=ping-key\n"), nil
		},
		Host: installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon.local", nil }},
		Engine: fstest.MapFS{
			"docker-compose.yml":            {Data: []byte("services: {}\n")},
			"docker-compose.monitoring.yml": {Data: []byte("services: {}\n")},
			"grafana/datasource.yml":        {Data: []byte("# fixture\n")},
			"prometheus/prometheus.yml":     {Data: []byte("# fixture\n")},
			"scripts/backup-excludes.txt":   {Data: []byte("logs\n")},
		},
		Compose: func(io.Writer, *compose.Outcomes) (composeRunner, error) { return composer, nil },
		Run:     func(_, _ io.Writer) commandRunner { return runner },
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			*pings = append(*pings, r.URL.Path)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("OK"))}, nil
		})},
		MachineIDFile: machineID,
		Now:           func() time.Time { return time.Date(2026, 10, 5, 4, 30, 0, 0, time.UTC) },
		Interactive:   func() bool { return interactive },
		Sleep:         func(time.Duration) { t.Fatal("no ping needs a retry") },
	}
}

func withoutSystemMachineID(t *testing.T, deps *Dependencies, without bool) {
	if without {
		deps.MachineIDFile = filepath.Join(t.TempDir(), "no-machine-id")
	}
}

func assertIdentityUnread(t *testing.T, unread bool, data string) {
	if unread {
		assert.NoFileExists(t, filepath.Join(data, ".machine-id"), "only the commands that need it read the machine's identity")
	}
}

func TestBackupCommandFlows(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	type Given struct {
		interactive bool
		stdin       string
	}
	type When struct {
		args []string
	}
	type Then struct {
		expect     func(r *mockCommandRunner, c *mockComposeRunner, data, tmp string)
		stdout     string
		stderr     string
		pings      []string
		noIdentity bool
	}
	backsUp := func(r *mockCommandRunner, c *mockComposeRunner, data string) {
		c.EXPECT().RunningServices(mock.Anything, project).Return([]string{"jellyfin"}, nil)
		r.EXPECT().Run(mock.Anything, resticCall("unlock")).Return(0, nil)
		c.EXPECT().Stop(mock.Anything, project).Return(nil)
		r.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
			want := []string{"backup", "--json", "--retry-lock", "2h", "--host", "gorgon", "--tag", "machine:this-machine", "--tag", "machine-name:gorgon", "--tag", "nightly", "--exclude-file"}
			return c.Dir == data && len(c.Args) == len(want)+2 && slices.Equal(want, c.Args[:len(want)]) &&
				filepath.Base(c.Args[len(want)]) == "backup-excludes.txt" && c.Args[len(want)+1] == "volumes" && len(c.ExtraFiles) == 1
		})).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
			_, _ = io.WriteString(c.Stdout, `{"message_type":"status","percent_done":1}`+"\n"+`{"message_type":"summary","snapshot_id":"40c4a929f0d1e2b3"}`+"\n")
			return 0, nil
		})
		c.EXPECT().Start(mock.Anything, project, []string{"jellyfin"}).Return(nil)
		r.EXPECT().Run(mock.Anything, resticCall("forget", "--json", "--retry-lock", "2h", "--host", "gorgon", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6")).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
			_, _ = io.WriteString(c.Stdout, `[{"keep":[{"short_id":"a"}],"remove":null}]`)
			return 0, nil
		})
	}
	backedUp := "Stopping the stack... done.\nBacking up volumes/... snapshot 40c4a929: 0 new, 0 changed, 0 unchanged files; 0 B added (0 B stored).\nStarting the services again... done.\nRemoving old snapshots... kept 1, removed 0.\nBackup done.\n"
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"backup": {
			When: When{args: []string{"backup"}},
			Then: Then{
				expect: func(r *mockCommandRunner, c *mockComposeRunner, data, _ string) {
					r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(ourSnapshots)}, nil)
					backsUp(r, c, data)
				},
				stdout: backedUp,
				pings:  []string{"/ping-key/gorgon-backup/start", "/ping-key/gorgon-backup"},
			},
		},
		"backup --take-over --yes": {
			When: When{args: []string{"backup", "--take-over", "--yes"}},
			Then: Then{
				expect: func(r *mockCommandRunner, c *mockComposeRunner, data, _ string) {
					r.EXPECT().Output(mock.Anything, resticCall("cat", "config", "--no-lock")).Return(process.Result{}, nil)
					r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(theirSnapshots)}, nil)
					backsUp(r, c, data)
				},
				stdout: backedUp + "This machine is now gorgon's main; backups from any other machine are refused.\n",
				pings:  []string{"/ping-key/gorgon-backup/start", "/ping-key/gorgon-backup"},
			},
		},
		"backup --take-over asks first": {
			Given: Given{interactive: true, stdin: "y\n"},
			When:  When{args: []string{"backup", "--take-over"}},
			Then: Then{
				expect: func(r *mockCommandRunner, c *mockComposeRunner, data, _ string) {
					r.EXPECT().Output(mock.Anything, resticCall("cat", "config", "--no-lock")).Return(process.Result{}, nil)
					r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(theirSnapshots)}, nil)
					backsUp(r, c, data)
				},
				stdout: backedUp + "This machine is now gorgon's main; backups from any other machine are refused.\n",
				stderr: "gorgon's main is pi2, last backup 2026-10-04 04:30. Taking over makes it refuse to back up.\nMake this machine the main instead? (y/n) ",
				pings:  []string{"/ping-key/gorgon-backup/start", "/ping-key/gorgon-backup"},
			},
		},
		"restore --overwrite": {
			When: When{args: []string{"restore", "--overwrite"}},
			Then: Then{
				noIdentity: true,
				stdout:     "previous volumes/ kept in <data>/volumes.before-restore-20261005-043000; delete it once the restore looks right\nRestoring volumes/ from the latest backup... restored.\n",
				expect: func(r *mockCommandRunner, c *mockComposeRunner, data, _ string) {
					c.EXPECT().AnyRunning(mock.Anything, project).Return(false, nil)
					r.EXPECT().Run(mock.Anything, resticCall("unlock")).Return(0, nil)
					r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(ourSnapshots)}, nil)
					r.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
						want := []string{"restore", "--retry-lock", "2h", "latest:/volumes", "--host", "gorgon", "--target", "", "--exclude", "configarr"}
						if len(c.Args) != len(want) {
							return false
						}
						target, _ := filepath.EvalSymlinks(c.Args[7])
						want[7] = c.Args[7]
						return slices.Equal(want, c.Args) && target == filepath.Join(data, "volumes")
					})).Return(0, nil)
				},
			},
		},
		"verify-backup": {
			When: When{args: []string{"verify-backup"}},
			Then: Then{
				expect: func(r *mockCommandRunner, _ *mockComposeRunner, _, tmp string) {
					r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(ourSnapshots)}, nil)
					r.EXPECT().Run(mock.Anything, resticCall("unlock")).Return(0, nil)
					r.EXPECT().Run(mock.Anything, resticCall("check", "--retry-lock", "2h")).Return(0, nil)
					r.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool {
						return slices.Contains(c.Args, "restore") && strings.HasPrefix(c.Args[slices.Index(c.Args, "--target")+1], tmp)
					})).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
						target := c.Args[slices.Index(c.Args, "--target")+1]
						require.NoError(t, os.MkdirAll(filepath.Join(target, "volumes"), 0o755))
						require.NoError(t, os.WriteFile(filepath.Join(target, "volumes", "notes.db"), []byte("not sqlite"), 0o644))
						return 0, nil
					})
				},
				stdout: "Checking the repository... no errors.\nRestoring the databases of the latest snapshot... restored 1 databases.\nChecking the databases... 1 intact.\nThe backups check out.\n",
				pings:  []string{"/ping-key/gorgon-verify/start", "/ping-key/gorgon-verify"},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home, data, resolved, tmp := backupHome(t)
			runner := newMockCommandRunner(t)
			composer := newMockComposeRunner(t)
			composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(project, nil).Maybe()
			tt.Then.expect(runner, composer, resolved, tmp)
			var pings []string
			deps := backupDependencies(t, home, tmp, runner, composer, tt.Given.interactive, &pings)
			withoutSystemMachineID(t, &deps, tt.Then.noIdentity)
			root := NewRootCommand(deps)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader(tt.Given.stdin))

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, 0, code, stderr.String())
			assert.Equal(t, strings.ReplaceAll(tt.Then.stdout, "<data>", data), stdout.String())
			assert.Equal(t, tt.Then.stderr, stderr.String())
			assert.Equal(t, tt.Then.pings, pings)
			assertIdentityUnread(t, tt.Then.noIdentity, data)
		})
	}
}

func TestBackupLogsResticButShowsSteps(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	for _, verbose := range []bool{false, true} {
		home, _, resolved, tmp := backupHome(t)
		runner := newMockCommandRunner(t)
		composer := newMockComposeRunner(t)
		composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(project, nil)
		runner.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(ourSnapshots)}, nil)
		composer.EXPECT().RunningServices(mock.Anything, project).Return(nil, nil)
		runner.EXPECT().Run(mock.Anything, resticCall("unlock")).Return(0, nil)
		composer.EXPECT().Stop(mock.Anything, project).Return(nil)
		runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool { return slices.Contains(c.Args, "backup") && c.Dir == resolved })).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
			_, _ = io.WriteString(c.Stdout, `{"message_type":"summary","snapshot_id":"40c4a929f0d1e2b3"}`+"\n")
			return 0, nil
		})
		runner.EXPECT().Run(mock.Anything, mock.MatchedBy(func(c process.Command) bool { return slices.Contains(c.Args, "forget") })).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
			_, _ = io.WriteString(c.Stdout, `[]`)
			return 0, nil
		})
		var pings []string
		root := NewRootCommand(backupDependencies(t, home, tmp, runner, composer, false, &pings))
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&bytes.Buffer{})
		args := []string{"backup"}
		if verbose {
			args = append(args, "--verbose")
		}

		code := run(context.Background(), root, args)

		assert.Equal(t, 0, code)
		logged, err := os.ReadFile(filepath.Join(home, ".local", "state", "mse", "gorgon", "logs", "mse.log"))
		require.NoError(t, err)
		assert.Contains(t, string(logged), "restic | snapshot 40c4a929")
		assert.Equal(t, verbose, strings.Contains(stdout.String(), "restic | snapshot 40c4a929"), "verbose %v", verbose)
	}
}

func TestBackupStopsWhenResticCannotBeFetched(t *testing.T) {
	home, _, _, tmp := backupHome(t)
	var pings []string
	runner := newMockCommandRunner(t)
	composer := newMockComposeRunner(t)
	composer.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&types.Project{Name: "media-server"}, nil).Maybe()
	deps := backupDependencies(t, home, tmp, runner, composer, false, &pings)
	deps.ResticBinary = func(context.Context) (string, error) {
		return "", errors.New("could not download restic 0.19.1 from github.com/restic/restic (no route to host)")
	}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"backup"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "could not download restic 0.19.1 from github.com/restic/restic (no route to host)")
}

func TestBackupFetchesResticBeforeWritingAnything(t *testing.T) {
	home, _, _, tmp := backupHome(t)
	var pings []string
	composer := newMockComposeRunner(t)
	composer.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&types.Project{Name: "media-server"}, nil).Maybe()
	deps := backupDependencies(t, home, tmp, newMockCommandRunner(t), composer, false, &pings)
	deps.ResticBinary = func(context.Context) (string, error) { return "", errors.New("offline") }
	root := NewRootCommand(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	run(context.Background(), root, []string{"backup"})

	assert.NoFileExists(t, filepath.Join(home, ".local", "state", "mse", "gorgon", "backup-excludes.txt"))
}
