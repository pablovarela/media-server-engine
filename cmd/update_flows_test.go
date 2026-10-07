package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/runs"
	"github.com/pablovarela/media-server-engine/internal/selfupdate"
	"github.com/pablovarela/media-server-engine/internal/version"
)

var released = version.Build{Version: "v0.12.2", Commit: "6437511", Date: "2026-10-05"}

type gitAnswers struct {
	changes  string
	remote   bool
	upstream int
	behind   string
}

func expectGit(runner *mockCommandRunner, config string, a gitAnswers) {
	git := func(args ...string) process.Command {
		return process.Command{Name: "git", Args: append([]string{"-C", config}, args...), Env: []string{"GIT_TERMINAL_PROMPT=0"}}
	}
	answer := func(stdout string, exit int) process.Result {
		return process.Result{Stdout: []byte(stdout), Exit: exit}
	}
	runner.EXPECT().Output(mock.Anything, git("status", "--porcelain", "--untracked-files=no")).Return(answer(a.changes, 0), nil)
	remote := 2
	if a.remote {
		remote = 0
	}
	runner.EXPECT().Output(mock.Anything, git("remote", "get-url", "origin")).Return(answer("", remote), nil)
	if !a.remote || a.changes != "" {
		return
	}
	runner.EXPECT().Output(mock.Anything, git("rev-parse", "--abbrev-ref", "@{upstream}")).Return(answer("origin/main\n", a.upstream), nil)
	if a.upstream != 0 {
		return
	}
	runner.EXPECT().Output(mock.Anything, git("fetch", "--quiet")).Return(answer("", 0), nil)
	runner.EXPECT().Output(mock.Anything, git("rev-list", "--count", "@{upstream}..HEAD")).Return(answer("0\n", 0), nil)
	runner.EXPECT().Output(mock.Anything, git("rev-list", "--count", "HEAD..@{upstream}")).Return(answer(a.behind, 0), nil)
	if a.behind != "0\n" {
		runner.EXPECT().Output(mock.Anything, git("merge", "--ff-only", "--quiet", "@{upstream}")).Return(answer("", 0), nil)
	}
}

func updatingTo(t *testing.T, to, path string) *mockUpdater {
	t.Helper()
	update := newMockUpdater(t)
	update.EXPECT().Update(mock.Anything, released, false, mock.Anything).Return(selfupdate.Result{From: released.Version, To: to, Path: path}, nil)
	return update
}

func TestUpdateFetchesTheConfig(t *testing.T) {
	type Then struct {
		code   int
		stdout string
		stderr []string
	}
	tests := map[string]struct {
		Given gitAnswers
		Then  Then
	}{
		"fast-forwards":      {Given: gitAnswers{remote: true, behind: "2\n"}, Then: Then{stdout: "Updating the config... took 2 commits.\n"}},
		"already up to date": {Given: gitAnswers{remote: true, behind: "0\n"}, Then: Then{stdout: "Updating the config... up to date.\n"}},
		"no remote":          {Given: gitAnswers{}, Then: Then{stdout: "Updating the config... no remote, not pulled.\n"}},
		"uncommitted changes": {
			Given: gitAnswers{changes: " M apps.yml\n", remote: true},
			Then: Then{code: 1, stderr: []string{
				"The config has changes that are not committed:\n M apps.yml\nSee them with: git -C <home>/.config/mse/gorgon diff\n",
				`git -C <home>/.config/mse/gorgon commit -am "<what changed>" && git -C <home>/.config/mse/gorgon push`,
			}},
		},
		"no upstream": {Given: gitAnswers{remote: true, upstream: 128}, Then: Then{code: 1, stderr: []string{"the config's branch tracks no remote branch"}}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t)
			expectGit(f.runner, filepath.Join(f.home, ".config", "mse", "gorgon"), tt.Given)
			deps := f.deps(t, false)
			deps.Build = released
			if tt.Then.code == 0 {
				deps.Update = updatingTo(t, "", "")
			}
			root := NewRootCommand(deps)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{"update"})

			assert.Equal(t, tt.Then.code, code, stderr.String())
			assert.Contains(t, stdout.String(), tt.Then.stdout)
			for _, line := range tt.Then.stderr {
				assert.Contains(t, stderr.String(), replaceHome(line, f.home))
			}
		})
	}
}

func TestUpdateSkipsTheSchemaCheckBeforeFetching(t *testing.T) {
	f := newApplyFixture(t)
	config := filepath.Join(f.home, ".config", "mse", "gorgon")
	require.NoError(t, os.WriteFile(filepath.Join(config, "config.yml"), []byte("config: 1\n"), 0o644))
	expectGit(f.runner, config, gitAnswers{})
	deps := f.deps(t, false)
	deps.Build = released
	deps.Update = updatingTo(t, "", "")
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"update"})

	assert.Equal(t, 0, code, stderr.String())
}

func TestUpdateAndApply(t *testing.T) {
	type Given struct {
		to      string
		execErr error
	}
	type Then struct {
		code  int
		exec  []string
		pings []string
	}
	tests := map[string]struct {
		Given Given
		When  []string
		Then  Then
	}{
		"a new binary takes over": {
			Given: Given{to: "v0.13.0"},
			When:  []string{"update", "--apply", "-v"},
			Then: Then{
				exec:  []string{"/opt/mse", "apply", "--after-update=<run id>", "--verbose"},
				pings: []string{"GET /ping-key/gorgon-update/start"},
			},
		},
		"the new binary cannot run": {
			Given: Given{to: "v0.13.0", execErr: errors.New("exec format error")},
			When:  []string{"update", "--apply"},
			Then: Then{
				code:  1,
				exec:  []string{"/opt/mse", "apply", "--after-update=<run id>"},
				pings: []string{"GET /ping-key/gorgon-update/start", "GET /ping-key/gorgon-update/fail"},
			},
		},
		"the same binary applies in-process": {
			When: []string{"update", "--apply"},
			Then: Then{pings: []string{"GET /ping-key/gorgon-update/start", "GET /ping-key/gorgon-update"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t)
			writeHealthchecksKeys(t, f)
			expectGit(f.runner, filepath.Join(f.home, ".config", "mse", "gorgon"), gitAnswers{})
			if tt.Given.to == "" {
				f.expectApply(nil)
			}
			deps := f.deps(t, false)
			deps.Build = released
			path := ""
			if tt.Given.to != "" {
				path = "/opt/mse"
			}
			deps.Update = updatingTo(t, tt.Given.to, path)
			var executed []string
			deps.Exec = func(path string, args []string) error {
				executed = args
				assert.Equal(t, "/opt/mse", path)
				return tt.Given.execErr
			}
			root := NewRootCommand(deps)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.When)

			assert.Equal(t, tt.Then.code, code, stderr.String())
			assert.Equal(t, tt.Then.pings, *f.requests)
			if tt.Then.exec != nil {
				id := runIDIn(t, f)
				want := strings.Split(strings.ReplaceAll(strings.Join(tt.Then.exec, "\x00"), "<run id>", id), "\x00")
				assert.Equal(t, want, executed)
			}
		})
	}
}

func writeHealthchecksKeys(t *testing.T, f applyFixture) {
	t.Helper()
	secrets := filepath.Join(f.home, ".local", "state", "mse", "gorgon", ".secrets")
	require.NoError(t, os.MkdirAll(secrets, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(secrets, "healthchecks.env"), []byte("HEALTHCHECKS_PING_KEY=ping-key\n"), 0o600))
}

func runIDIn(t *testing.T, f applyFixture) string {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(f.home, ".local", "state", "mse", "gorgon", "logs", "mse.log"))
	require.NoError(t, err)
	found := regexp.MustCompile(`update\[([0-9a-f]{6})\] start`).FindStringSubmatch(string(log))
	require.NotNil(t, found, string(log))
	return found[1]
}

func TestUpdateApplyWaitsForTheBackup(t *testing.T) {
	f := newApplyFixture(t)
	writeHealthchecksKeys(t, f)
	expectGit(f.runner, filepath.Join(f.home, ".config", "mse", "gorgon"), gitAnswers{})
	f.expectApply(nil)
	held, err := os.OpenFile(filepath.Join(f.data, ".backup.lock"), os.O_RDWR|os.O_CREATE, 0o644)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	deps := f.deps(t, false)
	deps.Build = released
	deps.Update = updatingTo(t, "", "")
	waits := 0
	deps.Pause = func(_ context.Context, d time.Duration) error {
		assert.Equal(t, 10*time.Second, d)
		waits++
		return held.Close()
	}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"update", "--apply"})

	assert.Equal(t, 0, code, stderr.String())
	assert.Equal(t, 1, waits)
	assert.Equal(t, 1, strings.Count(stdout.String(), "Waiting for the running backup to finish...\n"))
}

func TestUpdateApplyReportsFailWhenInterrupted(t *testing.T) {
	f := newApplyFixture(t)
	writeHealthchecksKeys(t, f)
	expectGit(f.runner, filepath.Join(f.home, ".config", "mse", "gorgon"), gitAnswers{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(f.project, nil)
	f.composer.EXPECT().Pull(mock.Anything, f.project).RunAndReturn(func(context.Context, *types.Project) (compose.Pulled, error) {
		cancel()
		return compose.Pulled{}, context.Canceled
	})
	deps := f.deps(t, false)
	deps.Build = released
	deps.Update = updatingTo(t, "", "")
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(ctx, root, []string{"update", "--apply"})

	assert.Equal(t, 1, code)
	assert.Equal(t, []string{"GET /ping-key/gorgon-update/start", "GET /ping-key/gorgon-update/fail"}, *f.requests)
}

func TestUpdateApplyReportsFailWhenInterruptedWaitingForTheBackup(t *testing.T) {
	f := newApplyFixture(t)
	writeHealthchecksKeys(t, f)
	held, err := os.OpenFile(filepath.Join(f.data, ".backup.lock"), os.O_RDWR|os.O_CREATE, 0o644)
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	deps := f.deps(t, false)
	deps.Build = released
	deps.Pause = func(context.Context, time.Duration) error { return context.Canceled }
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"update", "--apply"})

	assert.Equal(t, 1, code)
	assert.Equal(t, []string{"GET /ping-key/gorgon-update/start", "GET /ping-key/gorgon-update/fail"}, *f.requests)
}

func TestUpdateWithSeveralInstallationsUpdatesOnlyMse(t *testing.T) {
	_, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n", "trial": "INSTALLATION_NAME=trial\n"})
	deps := Dependencies{Build: released, Update: updatingTo(t, "", ""), Home: home, Environment: func(string) string { return "" }}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"update"})

	assert.Equal(t, 0, code, stderr.String())
	assert.Contains(t, stdout.String(), "Several installations here (gorgon, trial), and a machine runs one; only mse is updated. Remove the ones this machine shouldn't have.\n")
}

func TestUpdateApplyReportsFailWhenTheBackupKeepsRunning(t *testing.T) {
	f := newApplyFixture(t)
	writeHealthchecksKeys(t, f)
	held, err := os.OpenFile(filepath.Join(f.data, ".backup.lock"), os.O_RDWR|os.O_CREATE, 0o644)
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	deps := f.deps(t, false)
	deps.Build = released
	now := time.Date(2026, 10, 6, 5, 0, 0, 0, time.UTC)
	deps.Now = func() time.Time { return now }
	deps.Pause = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"update", "--apply"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "a backup is still running; run mse update --apply again once it has finished")
	assert.Equal(t, []string{"GET /ping-key/gorgon-update/start", "GET /ping-key/gorgon-update/fail"}, *f.requests)
}

func TestTheUpdateRecordSpansTheHandOver(t *testing.T) {
	f := newApplyFixture(t)
	expectGit(f.runner, filepath.Join(f.home, ".config", "mse", "gorgon"), gitAnswers{})
	deps := f.deps(t, false)
	deps.Build = released
	deps.Update = updatingTo(t, "v0.13.0", "/opt/mse")
	deps.Exec = func(string, []string) error { return nil }
	dir := filepath.Join(f.home, ".local", "state", "mse", "gorgon", "runs")

	require.Equal(t, 0, run(context.Background(), NewRootCommand(deps), []string{"update", "--apply"}))
	handedOver, recorded, err := runs.Read(dir, "update")
	require.NoError(t, err)
	require.True(t, recorded)
	assert.True(t, handedOver.Ended.IsZero(), "the update hands over before the run ends")

	f.expectApply(nil)
	require.Equal(t, 0, run(context.Background(), NewRootCommand(deps), []string{"apply", "--after-update=a1b2c3"}))
	applied, _, err := runs.Read(dir, "update")
	require.NoError(t, err)
	assert.True(t, applied.Started.Equal(handedOver.Started), "the apply ends the run the update started")
	assert.False(t, applied.Ended.IsZero())
	assert.False(t, applied.Failed)
}
