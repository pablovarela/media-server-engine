package gitconfig

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/pablovarela/media-server-engine/internal/process"
)

var (
	ErrDiverged   = errors.New("the config has commits its remote doesn't have; push them, or reset the config to its remote")
	ErrNoUpstream = errors.New("the config's branch tracks no remote branch")
)

const quiet = "--quiet"

type runner interface {
	Output(ctx context.Context, c process.Command) (process.Result, error)
}

type Repository struct {
	Runner runner
	Dir    string
}

func (r Repository) Changes(ctx context.Context) (string, error) {
	out, err := r.succeeding(ctx, "status", "--porcelain", "--untracked-files=no")
	return strings.TrimRight(out, "\n"), err
}

func (r Repository) HasRemote(ctx context.Context) (bool, error) {
	result, err := r.git(ctx, "remote", "get-url", "origin")
	return err == nil && result.Exit == 0, err
}

func (r Repository) FastForward(ctx context.Context) (int, error) {
	tracking, err := r.git(ctx, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err != nil {
		return 0, err
	}
	if tracking.Exit != 0 {
		return 0, ErrNoUpstream
	}
	if _, err := r.succeeding(ctx, "fetch", quiet); err != nil {
		return 0, err
	}
	ahead, err := r.count(ctx, "@{upstream}..HEAD")
	if err != nil {
		return 0, err
	}
	behind, err := r.count(ctx, "HEAD..@{upstream}")
	if err != nil || behind == 0 {
		return 0, err
	}
	if ahead > 0 {
		return 0, ErrDiverged
	}
	_, err = r.succeeding(ctx, "merge", "--ff-only", quiet, "@{upstream}")
	return behind, err
}

func (r Repository) HasIdentity(ctx context.Context) (bool, error) {
	for _, key := range []string{"user.name", "user.email"} {
		result, err := r.git(ctx, "config", key)
		if err != nil {
			return false, err
		}
		if result.Exit != 0 || strings.TrimSpace(string(result.Stdout)) == "" {
			return false, nil
		}
	}
	return true, nil
}

func (r Repository) Commit(ctx context.Context, message string, paths []string) (string, error) {
	if _, err := r.succeeding(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return "", err
	}
	if _, err := r.succeeding(ctx, append([]string{"commit", quiet, "-m", message, "--"}, paths...)...); err != nil {
		return "", err
	}
	sha, err := r.succeeding(ctx, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(sha), err
}

func (r Repository) Push(ctx context.Context) error {
	_, err := r.succeeding(ctx, "push", quiet)
	return err
}

func (r Repository) Init(ctx context.Context) error {
	_, err := r.succeeding(ctx, "init", quiet, "--initial-branch=main")
	return err
}

func (r Repository) AddRemote(ctx context.Context, url string) error {
	_, err := r.succeeding(ctx, "remote", "add", "origin", url)
	return err
}

func (r Repository) PushNew(ctx context.Context) error {
	_, err := r.succeeding(ctx, "push", quiet, "--set-upstream", "origin", "main")
	return err
}

func (r Repository) Unstage(ctx context.Context, paths []string) error {
	_, err := r.succeeding(ctx, append([]string{"reset", quiet, "--"}, paths...)...)
	return err
}

func (r Repository) RemoteURL(ctx context.Context) (string, error) {
	out, err := r.succeeding(ctx, "remote", "get-url", "origin")
	return strings.TrimSpace(out), err
}

func DisplayRemote(remote string) string {
	if parsed, err := url.Parse(remote); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return strings.TrimSuffix(parsed.Host+parsed.Path, ".git")
	}
	if user, rest, found := strings.Cut(remote, "@"); found && !strings.Contains(user, "/") {
		host, path, _ := strings.Cut(rest, ":")
		return strings.TrimSuffix(host+"/"+path, ".git")
	}
	return remote
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
		return "", failure(args, result)
	}
	return string(result.Stdout), nil
}

func failure(args []string, result process.Result) error {
	message := fmt.Sprintf("git %s failed (exit %d)", strings.Join(args[:min(2, len(args))], " "), result.Exit)
	if detail := strings.TrimSpace(string(result.Stderr)); detail != "" {
		message += ": " + detail
	}
	return errors.New(message)
}

func Clone(ctx context.Context, r runner, url, dir string) error {
	args := []string{"clone", quiet, "--", url, dir}
	result, err := r.Output(ctx, process.Command{Name: "git", Args: args, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	if err != nil {
		return err
	}
	if result.Exit != 0 {
		return failure(args[:1], result)
	}
	return nil
}

func (r Repository) git(ctx context.Context, args ...string) (process.Result, error) {
	return r.Runner.Output(ctx, process.Command{Name: "git", Args: append([]string{"-C", r.Dir}, args...), Env: []string{"GIT_TERMINAL_PROMPT=0"}})
}
