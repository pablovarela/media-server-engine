package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
)

func TestBackupTakeOverRefusals(t *testing.T) {
	asksAboutTheMain := func(r *mockCommandRunner, c *mockComposeRunner) {
		c.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(&types.Project{Name: "media-server"}, nil).Once()
		r.EXPECT().Output(mock.Anything, resticCall("cat", "config", "--no-lock")).Return(process.Result{}, nil).Once()
		r.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Stdout: []byte(theirSnapshots)}, nil).Once()
	}
	type Given struct {
		interactive bool
		stdin       string
		expect      func(r *mockCommandRunner, c *mockComposeRunner)
	}
	type When struct {
		args []string
	}
	type Then struct {
		stderr string
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"declined": {
			Given: Given{interactive: true, stdin: "n\n", expect: asksAboutTheMain},
			When:  When{args: []string{"backup", "--apps", "--take-over"}},
			Then:  Then{stderr: "mse: nothing was claimed\n"},
		},
		"no one to answer": {
			Given: Given{expect: asksAboutTheMain},
			When:  When{args: []string{"backup", "--apps", "--take-over"}},
			Then:  Then{stderr: "mse: nothing was claimed; mse backup --apps --take-over --yes takes over without asking\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home, _, _, tmp := backupHome(t)
			runner := newMockCommandRunner(t)
			composer := newMockComposeRunner(t)
			if tt.Given.expect != nil {
				tt.Given.expect(runner, composer)
			}
			var pings []string
			root := NewRootCommand(backupDependencies(t, home, tmp, runner, composer, tt.Given.interactive, &pings))
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader(tt.Given.stdin))

			code := run(context.Background(), root, tt.When.args)

			assert.Equal(t, 1, code)
			assert.True(t, strings.HasSuffix(stderr.String(), tt.Then.stderr), stderr.String())
			assert.Empty(t, pings)
		})
	}
}

func TestBackupAndRestoreSayWhat(t *testing.T) {
	tests := map[string]struct {
		args   []string
		stderr string
	}{
		"backup alone":               {args: []string{"backup"}, stderr: "mse: say what to back up: --apps, --media, or both\n"},
		"restore alone":              {args: []string{"restore"}, stderr: "mse: say what to restore: --apps, --media, or both\n"},
		"--take-over without --apps": {args: []string{"backup", "--take-over"}, stderr: "mse: --take-over only goes with --apps\n"},
		"--overwrite without --apps": {args: []string{"restore", "--overwrite"}, stderr: "mse: --overwrite only goes with --apps\n"},
		"--yes without --take-over":  {args: []string{"backup", "--apps", "--yes"}, stderr: "mse: --yes only goes with --take-over\n"},
		"--media when it is off":     {args: []string{"backup", "--media"}, stderr: "mse: gorgon has no media backup; turn it on with mse configure\n"},
		"restore --media when off":   {args: []string{"restore", "--media"}, stderr: "mse: gorgon has no media backup; turn it on with mse configure\n"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			home, _, _, tmp := backupHome(t)
			var pings []string
			root := NewRootCommand(backupDependencies(t, home, tmp, newMockCommandRunner(t), newMockComposeRunner(t), false, &pings))
			var stderr bytes.Buffer
			root.SetOut(io.Discard)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.args)

			assert.Equal(t, 1, code)
			assert.Equal(t, tt.stderr, stderr.String())
			assert.Empty(t, pings)
		})
	}
}

func TestAFailedAppsBackupSkipsTheMedia(t *testing.T) {
	home, _, _, tmp := backupHome(t)
	withSettings(t, home, mediaSettings)
	runner := newMockCommandRunner(t)
	composer := newMockComposeRunner(t)
	composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(&types.Project{Name: "media-server"}, nil)
	runner.EXPECT().Output(mock.Anything, resticCall("snapshots", "--no-lock", "--host", "gorgon", "--json")).Return(process.Result{Exit: 1, Stderr: []byte("Fatal: unreachable\n")}, nil)
	var pings []string
	root := NewRootCommand(backupDependencies(t, home, tmp, runner, composer, false, &pings))
	var stderr bytes.Buffer
	root.SetOut(io.Discard)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"backup", "--apps", "--media"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "Fatal: unreachable")
	assert.Equal(t, []string{"/ping-key/gorgon-backup/start", "/ping-key/gorgon-backup/fail"}, pings)
}

func localRestic(context.Context) (string, error) { return "restic", nil }

func resticCall(args ...string) any {
	return mock.MatchedBy(func(c process.Command) bool {
		return c.Name == "restic" && slices.Equal(args, c.Args) &&
			slices.Contains(c.Env, "RESTIC_REPOSITORY=b2:bucket") && slices.Contains(c.Env, "RESTIC_PASSWORD=secret")
	})
}

func TestBackupCommandsNeedARepository(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{
		ResticBinary: localRestic,
		Environment:  getenv, Home: home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) { return []byte("RESTIC_PASSWORD=secret\n"), nil },
		Host:    installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon", nil }},
		Run:     func(_, _ io.Writer) commandRunner { return newMockCommandRunner(t) },
	})
	var stderr bytes.Buffer
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"check-backup"})

	assert.Equal(t, 1, code)
	assert.Equal(t, "mse: gorgon has no backup repository: set RESTIC_REPOSITORY in installation.env\n", stderr.String())
}

func resticArgs(first string) any {
	return mock.MatchedBy(func(c process.Command) bool { return c.Name == "restic" && len(c.Args) > 0 && c.Args[0] == first })
}

func TestRestoreMediaThenStarts(t *testing.T) {
	tests := map[string]struct {
		args     []string
		restores []string
	}{
		"the media":                {args: []string{"restore", "--media"}, restores: []string{"media"}},
		"the apps, then the media": {args: []string{"restore", "--apps", "--media"}, restores: []string{"apps", "media"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t)
			withSettings(t, f.home, mediaSettings)
			require.NoError(t, os.RemoveAll(filepath.Join(f.data, ".backup-main")))
			var restores []string
			f.composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(f.project, nil).Maybe()
			f.composer.EXPECT().AnyRunning(mock.Anything, f.project).Return(false, nil)
			f.runner.EXPECT().Output(mock.Anything, resticArgs("snapshots")).Return(process.Result{Stdout: []byte(ourSnapshots)}, nil)
			f.runner.EXPECT().Run(mock.Anything, resticArgs("unlock")).Return(0, nil)
			f.runner.EXPECT().Run(mock.Anything, resticArgs("restore")).RunAndReturn(func(_ context.Context, c process.Command) (int, error) {
				which := "apps"
				if slices.Contains(c.Env, "RESTIC_REPOSITORY=b2:bucket:media") {
					which = "media"
					assert.Equal(t, []string{"--overwrite", "if-changed"}, c.Args[len(c.Args)-2:])
				}
				restores = append(restores, which)
				return 0, nil
			})
			f.composer.EXPECT().Pull(mock.Anything, f.project).Return(compose.Pulled{}, nil)
			f.composer.EXPECT().Up(mock.Anything, f.project, []string(nil), compose.NoWait).RunAndReturn(func(context.Context, *types.Project, []string, compose.Wait) error {
				assert.Equal(t, tt.restores, restores, "the stack starts after every restore")
				return nil
			})
			f.composer.EXPECT().Detached(mock.Anything, f.project, "gluetun", gluetunDependents).Return(nil, nil)
			f.composer.EXPECT().Ps(mock.Anything, f.project).Return(nil, nil)
			root := NewRootCommand(f.deps(t, false))
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.args)

			assert.Equal(t, 0, code, stderr.String())
			assert.Equal(t, tt.restores, restores)
			media, _ := filepath.EvalSymlinks(filepath.Join(f.data, "data", "media"))
			assert.Contains(t, stdout.String(), "Restoring the media from the latest media backup into "+media+" (")
			assert.Contains(t, stdout.String(), "files already there that match are kept)... restored.\n")
			assert.Contains(t, stdout.String(), "Starting the stack...")
		})
	}
}
