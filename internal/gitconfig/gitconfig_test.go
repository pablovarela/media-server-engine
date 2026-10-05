package gitconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/process"
)

const dir = "/config/gorgon"

func git(args ...string) process.Command {
	return process.Command{Name: "git", Args: append([]string{"-C", dir}, args...), Env: []string{"GIT_TERMINAL_PROMPT=0"}}
}

func answer(stdout string, exit int) process.Result {
	return process.Result{Stdout: []byte(stdout), Exit: exit}
}

func TestChanges(t *testing.T) {
	ctx := context.Background()
	runner := newMockRunner(t)
	runner.EXPECT().Output(ctx, git("status", "--porcelain", "--untracked-files=no")).Return(answer(" M apps.yml\n D prowlarr.yml\n", 0), nil)

	changes, err := Repository{Runner: runner, Dir: dir}.Changes(ctx)

	require.NoError(t, err)
	assert.Equal(t, " M apps.yml\n D prowlarr.yml", changes)
}

func TestHasRemote(t *testing.T) {
	ctx := context.Background()
	for name, exit := range map[string]int{"with origin": 0, "without origin": 2} {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Output(ctx, git("remote", "get-url", "origin")).Return(answer("", exit), nil)

			has, err := Repository{Runner: runner, Dir: dir}.HasRemote(ctx)

			require.NoError(t, err)
			assert.Equal(t, exit == 0, has)
		})
	}
}

func TestFastForward(t *testing.T) {
	ctx := context.Background()
	type Given struct {
		upstream  process.Result
		ahead     string
		behind    string
		mergeExit int
	}
	type Then struct {
		taken int
		err   error
		msg   string
	}
	tracked := answer("origin/main\n", 0)
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"already up to date": {Given: Given{upstream: tracked, ahead: "0\n", behind: "0\n"}, Then: Then{taken: 0}},
		"behind":             {Given: Given{upstream: tracked, ahead: "0\n", behind: "2\n"}, Then: Then{taken: 2}},
		"diverged":           {Given: Given{upstream: tracked, ahead: "1\n", behind: "2\n"}, Then: Then{err: ErrDiverged}},
		"only ahead":         {Given: Given{upstream: tracked, ahead: "1\n", behind: "0\n"}, Then: Then{taken: 0}},
		"no upstream": {
			Given: Given{upstream: process.Result{Exit: 128, Stderr: []byte("fatal: no upstream configured for branch 'main'\n")}},
			Then:  Then{err: ErrNoUpstream},
		},
		"merge refused": {
			Given: Given{upstream: tracked, ahead: "0\n", behind: "1\n", mergeExit: 1},
			Then:  Then{msg: "git merge --ff-only failed (exit 1)"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Output(ctx, git("rev-parse", "--abbrev-ref", "@{upstream}")).Return(tt.Given.upstream, nil)
			if tt.Given.ahead != "" {
				runner.EXPECT().Output(ctx, git("fetch", "--quiet")).Return(answer("", 0), nil)
				runner.EXPECT().Output(ctx, git("rev-list", "--count", "@{upstream}..HEAD")).Return(answer(tt.Given.ahead, 0), nil)
			}
			if tt.Given.behind != "" {
				runner.EXPECT().Output(ctx, git("rev-list", "--count", "HEAD..@{upstream}")).Return(answer(tt.Given.behind, 0), nil)
			}
			if tt.Given.behind != "" && tt.Given.behind != "0\n" && tt.Given.ahead == "0\n" {
				runner.EXPECT().Output(ctx, git("merge", "--ff-only", "--quiet", "@{upstream}")).Return(answer("", tt.Given.mergeExit), nil)
			}

			taken, err := Repository{Runner: runner, Dir: dir}.FastForward(ctx)

			switch {
			case tt.Then.err != nil:
				assert.ErrorIs(t, err, tt.Then.err)
			case tt.Then.msg != "":
				assert.EqualError(t, err, tt.Then.msg)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.Then.taken, taken)
			}
		})
	}
}

func TestFastForwardWhenGitCannotRun(t *testing.T) {
	ctx := context.Background()
	runner := newMockRunner(t)
	runner.EXPECT().Output(ctx, git("rev-parse", "--abbrev-ref", "@{upstream}")).Return(process.Result{}, errors.New("exec: \"git\": executable file not found in $PATH"))

	_, err := Repository{Runner: runner, Dir: dir}.FastForward(ctx)

	assert.EqualError(t, err, "exec: \"git\": executable file not found in $PATH")
}

func TestHasIdentity(t *testing.T) {
	ctx := context.Background()
	tests := map[string]struct {
		name, email process.Result
		want        bool
	}{
		"both set":    {name: answer("Pablo\n", 0), email: answer("p@example.com\n", 0), want: true},
		"no name":     {name: answer("", 1), email: answer("p@example.com\n", 0)},
		"no email":    {name: answer("Pablo\n", 0), email: answer("", 1)},
		"empty email": {name: answer("Pablo\n", 0), email: answer("\n", 0)},
	}
	for label, tt := range tests {
		t.Run(label, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Output(ctx, git("config", "user.name")).Return(tt.name, nil)
			runner.EXPECT().Output(ctx, git("config", "user.email")).Return(tt.email, nil).Maybe()

			has, err := Repository{Runner: runner, Dir: dir}.HasIdentity(ctx)

			require.NoError(t, err)
			assert.Equal(t, tt.want, has)
		})
	}
}

func TestCommit(t *testing.T) {
	ctx := context.Background()
	paths := []string{"installation.env", "secrets/vpn.sops.env"}
	message := "Configure gorgon: General, VPN"
	t.Run("committed", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Output(ctx, git("add", "--", "installation.env", "secrets/vpn.sops.env")).Return(answer("", 0), nil).Once()
		runner.EXPECT().Output(ctx, git("commit", "--quiet", "-m", message, "--", "installation.env", "secrets/vpn.sops.env")).Return(answer("", 0), nil).Once()
		runner.EXPECT().Output(ctx, git("rev-parse", "--short", "HEAD")).Return(answer("a1b2c3d\n", 0), nil).Once()

		sha, err := Repository{Runner: runner, Dir: dir}.Commit(ctx, message, paths)

		require.NoError(t, err)
		assert.Equal(t, "a1b2c3d", sha)
	})
	t.Run("failing", func(t *testing.T) {
		runner := newMockRunner(t)
		runner.EXPECT().Output(ctx, git("add", "--", "installation.env", "secrets/vpn.sops.env")).Return(answer("", 0), nil).Once()
		runner.EXPECT().Output(ctx, git("commit", "--quiet", "-m", message, "--", "installation.env", "secrets/vpn.sops.env")).
			Return(process.Result{Exit: 1, Stderr: []byte("Author identity unknown\n")}, nil).Once()

		_, err := Repository{Runner: runner, Dir: dir}.Commit(ctx, message, paths)

		require.EqualError(t, err, "git commit --quiet failed (exit 1): Author identity unknown")
	})
}

func TestPush(t *testing.T) {
	ctx := context.Background()
	for name, tt := range map[string]struct {
		result process.Result
		err    string
	}{
		"pushed":   {result: answer("", 0)},
		"rejected": {result: process.Result{Exit: 1, Stderr: []byte("! [rejected] main -> main (fetch first)\n")}, err: "git push --quiet failed (exit 1): ! [rejected] main -> main (fetch first)"},
	} {
		t.Run(name, func(t *testing.T) {
			runner := newMockRunner(t)
			runner.EXPECT().Output(ctx, git("push", "--quiet")).Return(tt.result, nil).Once()

			err := Repository{Runner: runner, Dir: dir}.Push(ctx)

			if tt.err == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.err)
			}
		})
	}
}

func TestRemoteURL(t *testing.T) {
	ctx := context.Background()
	runner := newMockRunner(t)
	runner.EXPECT().Output(ctx, git("remote", "get-url", "origin")).Return(answer("git@github.com:pablovarela/cfg.git\n", 0), nil).Once()

	url, err := Repository{Runner: runner, Dir: dir}.RemoteURL(ctx)

	require.NoError(t, err)
	assert.Equal(t, "git@github.com:pablovarela/cfg.git", url)
}

func TestDisplayRemote(t *testing.T) {
	for given, want := range map[string]string{
		"https://x-access-token:abc@github.com/pablovarela/cfg.git": "github.com/pablovarela/cfg",
		"https://github.com/pablovarela/cfg":                        "github.com/pablovarela/cfg",
		"git@github.com:pablovarela/cfg.git":                        "github.com/pablovarela/cfg",
		"ssh://git@github.com/pablovarela/cfg":                      "github.com/pablovarela/cfg",
		"/srv/git/cfg.git":                                          "/srv/git/cfg.git",
	} {
		t.Run(given, func(t *testing.T) {
			assert.Equal(t, want, DisplayRemote(given))
		})
	}
}

func TestUnstage(t *testing.T) {
	ctx := context.Background()
	runner := newMockRunner(t)
	runner.EXPECT().Output(ctx, git("reset", "--quiet", "--", "installation.env", "secrets/new.sops.env")).Return(answer("", 0), nil).Once()

	require.NoError(t, Repository{Runner: runner, Dir: dir}.Unstage(ctx, []string{"installation.env", "secrets/new.sops.env"}))
}
