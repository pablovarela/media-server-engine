package github

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

type TokenSource struct {
	Getenv  func(string) string
	GHToken func(context.Context) ([]byte, error)
}

func SystemTokenSource() TokenSource {
	return TokenSource{
		Getenv: os.Getenv,
		GHToken: func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "gh", "auth", "token").Output()
		},
	}
}

func (s TokenSource) Token(ctx context.Context) (string, error) {
	if token := s.Getenv("GITHUB_TOKEN"); token != "" {
		return token, nil
	}
	if out, err := s.GHToken(ctx); err == nil {
		if token := strings.TrimSpace(string(out)); token != "" {
			return token, nil
		}
	}
	return "", errors.New("set GITHUB_TOKEN to a token that can read " + repository + ", or log in with gh auth login")
}
