package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
		return "", fmt.Errorf("find the GitHub user: %w", c.explain(err, "can read its user"))
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
	return false, fmt.Errorf("look up %s/%s: %w", owner, repo, c.explain(err, "can read "+owner+"/"+repo))
}

func (c *Client) CreatePrivateRepository(ctx context.Context, org, repo string) (string, error) {
	api, err := c.authenticated(ctx)
	if err != nil {
		return "", err
	}
	private := true
	created, _, err := api.Repositories.Create(ctx, org, &gh.Repository{Name: &repo, Private: &private})
	switch {
	case err == nil:
		return created.GetCloneURL(), nil
	case taken(err):
		return "", fmt.Errorf("create %s: %w", repo, ErrRepositoryTaken)
	}
	return "", fmt.Errorf("create %s: %w", repo, c.explain(err, "can create repositories"))
}

func taken(err error) bool {
	var answer *gh.ErrorResponse
	if !errors.As(err, &answer) || answer.Response == nil || answer.Response.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	for _, e := range answer.Errors {
		if e.Field == "name" && strings.Contains(e.Message, "already exists") {
			return true
		}
	}
	return false
}
