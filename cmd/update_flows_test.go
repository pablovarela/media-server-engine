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
	runner.EXPECT().Output(mock.Anything, git("status", "--porcelain")).Return(answer(a.changes, 0), nil)
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
			When:  []string{"update", "--apply", "--installation", "gorgon", "-v"},
			Then: Then{
				exec:  []string{"/opt/mse", "apply", "--after-update=<run id>", "--installation", "gorgon", "--verbose"},
				pings: []string{"GET /ping-key/gorgon-update/start"},
			},
		},
		"the new binary cannot run": {
			Given: Given{to: "v0.13.0", execErr: errors.New("exec format error")},
			When:  []string{"update", "--apply"},
			Then: Then{
				code:  1,
				exec:  []string{"/opt/mse", "apply", "--after-update=<run id>", "--installation", "gorgon"},
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
	deps.Sleep = func(d time.Duration) {
		assert.Equal(t, 10*time.Second, d)
		waits++
		_ = held.Close()
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
	f.composer.EXPECT().Pull(mock.Anything, f.project).RunAndReturn(func(context.Context, *types.Project) error {
		cancel()
		return context.Canceled
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
