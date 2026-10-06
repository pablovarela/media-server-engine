package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v92/github"
)

var ErrRepositoryTaken = errors.New("the repository already exists")

func (c *Client) Login(ctx context.Context) (string, error) {
	api, err := c.authenticated(ctx)
	if err != nil {
		return "", err
	}
	user, _, err := api.Users.Get(ctx, "")
	if err != nil {
		return "", fmt.Errorf("find the GitHub user: %w", c.answered(err))
	}
	return user.GetLogin(), nil
}

func (c *Client) RepositoryExists(ctx context.Context, owner, repo string) (bool, error) {
	api, err := c.authenticated(ctx)
	if err != nil {
		return false, err
	}
	_, response, err := api.Repositories.Get(ctx, owner, repo)
	switch {
	case err == nil:
		return true, nil
	case response != nil && response.StatusCode == http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("look up %s/%s: %w", owner, repo, c.answered(err))
}

func (c *Client) CreatePrivateRepository(ctx context.Context, org, repo string) (string, error) {
	api, err := c.authenticated(ctx)
	if err != nil {
		return "", err
	}
	private := true
	created, response, err := api.Repositories.Create(ctx, org, &gh.Repository{Name: &repo, Private: &private})
	switch {
	case err == nil:
		return created.GetCloneURL(), nil
	case response != nil && response.StatusCode == http.StatusUnprocessableEntity:
		return "", fmt.Errorf("create %s: %w", repo, ErrRepositoryTaken)
	}
	return "", fmt.Errorf("create %s: %w", repo, c.answered(err))
}

func (c *Client) answered(err error) error {
	var answer *gh.ErrorResponse
	if !errors.As(err, &answer) || answer.Response == nil {
		return err
	}
	switch answer.Response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("GitHub answered %s: check the token from %s (gh auth login)", answer.Response.Status, c.tokenSource)
	default:
		return fmt.Errorf("GitHub answered %s", answer.Response.Status)
	}
}
