package gitconfig

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

var (
	ErrDiverged   = errors.New("the config has commits its remote doesn't have; push them, or reset the config to its remote")
	ErrNoUpstream = errors.New("the config's branch tracks no remote branch")
)

type runner interface {
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type Repository struct {
	Runner runner
	Dir    string
}

func (r Repository) Changes(ctx context.Context) (string, error) {
	out, err := r.succeeding(ctx, "status", "--porcelain")
	return strings.TrimRight(out, "\n"), err
}

func (r Repository) HasRemote(ctx context.Context) (bool, error) {
	result, err := r.git(ctx, "remote", "get-url", "origin")
	return err == nil && result.Exit == 0, err
}

func (r Repository) FastForward(ctx context.Context) (int, error) {
	if result, err := r.git(ctx, "rev-parse", "--abbrev-ref", "@{upstream}"); err != nil || result.Exit != 0 {
		return 0, errors.Join(err, ErrNoUpstream)
	}
	if _, err := r.succeeding(ctx, "fetch", "--quiet"); err != nil {
		return 0, err
	}
	ahead, err := r.count(ctx, "@{upstream}..HEAD")
	if err != nil {
		return 0, err
	}
	if ahead > 0 {
		return 0, ErrDiverged
	}
	behind, err := r.count(ctx, "HEAD..@{upstream}")
	if err != nil || behind == 0 {
		return 0, err
	}
	_, err = r.succeeding(ctx, "merge", "--ff-only", "--quiet", "@{upstream}")
	return behind, err
}

func (r Repository) count(ctx context.Context, revisions string) (int, error) {
	out, err := r.succeeding(ctx, "rev-list", "--count", revisions)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

func (r Repository) succeeding(ctx context.Context, args ...string) (string, error) {
	result, err := r.git(ctx, args...)
	if err != nil {
		return "", err
	}
	if result.Exit != 0 {
		message := fmt.Sprintf("git %s failed (exit %d)", strings.Join(args[:min(2, len(args))], " "), result.Exit)
		if detail := strings.TrimSpace(string(result.Stderr)); detail != "" {
			message += ": " + detail
		}
		return "", errors.New(message)
	}
	return string(result.Stdout), nil
}

func (r Repository) git(ctx context.Context, args ...string) (process.Result, error) {
	return r.Runner.Output(ctx, process.Command{Name: "git", Args: append([]string{"-C", r.Dir}, args...), Env: []string{"GIT_TERMINAL_PROMPT=0"}})
}
