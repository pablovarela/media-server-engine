package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	gh "github.com/google/go-github/v92/github"
)

const (
	owner      = "pablovarela"
	name       = "media-server-engine"
	repository = owner + "/" + name
)

type Asset struct {
	ID   int64
	Name string
}

type Release struct {
	Tag    string
	URL    string
	Assets []Asset
}

type Client struct {
	tokens      TokenSource
	tokenSource string
	http        *http.Client
	api         *gh.Client
}

func NewClient(tokens TokenSource, httpClient *http.Client) *Client {
	return &Client{tokens: tokens, http: httpClient}
}

func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	api, err := c.authenticated(ctx)
	if err != nil {
		return nil, err
	}
	all, _, err := api.Repositories.ListReleases(ctx, owner, name, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("list the releases of %s: %w", repository, c.explain(err))
	}
	var published []Release
	for _, release := range all {
		if !release.GetDraft() && !release.GetPrerelease() {
			published = append(published, releaseOf(release))
		}
	}
	return published, nil
}

func (c *Client) Download(ctx context.Context, assetID int64, w io.Writer) error {
	api, err := c.authenticated(ctx)
	if err != nil {
		return err
	}
	body, _, err := api.Repositories.DownloadReleaseAsset(ctx, owner, name, assetID, c.http)
	if err != nil {
		return fmt.Errorf("download asset %d: %w", assetID, c.explain(err))
	}
	defer func() { _ = body.Close() }()
	if _, err := io.Copy(w, body); err != nil {
		return fmt.Errorf("download asset %d: %w", assetID, err)
	}
	return nil
}

func (c *Client) authenticated(ctx context.Context) (*gh.Client, error) {
	if c.api != nil {
		return c.api, nil
	}
	token, source, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	api, err := gh.NewClient(gh.WithHTTPClient(c.http), gh.WithAuthToken(token))
	if err != nil {
		return nil, err
	}
	c.api, c.tokenSource = api, source
	return api, nil
}

func (c *Client) explain(err error) error {
	var answer *gh.ErrorResponse
	if !errors.As(err, &answer) || answer.Response == nil {
		return err
	}
	switch answer.Response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return fmt.Errorf("GitHub answered %s: check the token from %s can read %s", answer.Response.Status, c.tokenSource, repository)
	default:
		return fmt.Errorf("GitHub answered %s", answer.Response.Status)
	}
}

func releaseOf(release *gh.RepositoryRelease) Release {
	converted := Release{Tag: release.GetTagName(), URL: release.GetHTMLURL()}
	for _, asset := range release.Assets {
		converted.Assets = append(converted.Assets, Asset{ID: asset.GetID(), Name: asset.GetName()})
	}
	return converted
}
