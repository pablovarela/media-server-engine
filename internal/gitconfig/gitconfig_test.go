package gitconfig

import (
	"context"
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
	runner.EXPECT().Output(ctx, git("status", "--porcelain")).Return(answer(" M apps.yml\n?? notes.txt\n", 0), nil)

	changes, err := Repository{Runner: runner, Dir: dir}.Changes(ctx)

	require.NoError(t, err)
	assert.Equal(t, " M apps.yml\n?? notes.txt", changes)
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
		"diverged":           {Given: Given{upstream: tracked, ahead: "1\n"}, Then: Then{err: ErrDiverged}},
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
			if tt.Given.behind != "" && tt.Given.behind != "0\n" {
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
