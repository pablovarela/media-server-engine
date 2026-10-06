package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v92/github"
)

const (
	owner      = "pablovarela"
	name       = "media-server-engine"
	repository = owner + "/" + name
)

const EngineRepository = repository

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
	public      *gh.Client
}

func NewClient(tokens TokenSource, httpClient *http.Client) *Client {
	return &Client{tokens: tokens, http: httpClient}
}

func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	api, err := c.anonymous()
	if err != nil {
		return nil, err
	}
	all, _, err := api.Repositories.ListReleases(ctx, owner, name, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("list the releases of %s: %w", repository, answered(err))
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
	api, err := c.anonymous()
	if err != nil {
		return err
	}
	body, _, err := api.Repositories.DownloadReleaseAsset(ctx, owner, name, assetID, c.http)
	if err != nil {
		return fmt.Errorf("download asset %d: %w", assetID, answered(err))
	}
	defer func() { _ = body.Close() }()
	if _, err := io.Copy(w, body); err != nil {
		return fmt.Errorf("download asset %d: %w", assetID, err)
	}
	return nil
}

func (c *Client) anonymous() (*gh.Client, error) {
	if c.public != nil {
		return c.public, nil
	}
	api, err := gh.NewClient(gh.WithHTTPClient(c.http))
	if err != nil {
		return nil, err
	}
	c.public = api
	return api, nil
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

func answered(err error) error {
	answer, said, _ := saidBy(err)
	if answer == nil {
		return err
	}
	return errors.New(said)
}

func saidBy(err error) (answer *gh.ErrorResponse, said, reason string) {
	if !errors.As(err, &answer) || answer.Response == nil {
		return nil, "", ""
	}
	said = "GitHub answered " + answer.Response.Status
	reason = reasonOf(answer)
	if strings.Contains(answer.Response.Status, reason) {
		reason = ""
	}
	if reason != "" {
		said += ": " + reason
	}
	return answer, said, reason
}

func (c *Client) explain(err error, can string) error {
	answer, said, reason := saidBy(err)
	if answer == nil {
		return err
	}
	switch answer.Response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		separator := ": "
		if reason != "" {
			separator = "; "
		}
		return fmt.Errorf("%s%scheck the token from %s %s", said, separator, c.tokenSource, can)
	default:
		return errors.New(said)
	}
}

func reasonOf(answer *gh.ErrorResponse) string {
	reason := answer.Message
	var details []string
	for _, e := range answer.Errors {
		if e.Message != "" {
			details = append(details, e.Message)
		}
	}
	if len(details) > 0 {
		reason = strings.TrimSpace(reason + " (" + strings.Join(details, "; ") + ")")
	}
	return reason
}

func releaseOf(release *gh.RepositoryRelease) Release {
	converted := Release{Tag: release.GetTagName(), URL: release.GetHTMLURL()}
	for _, asset := range release.Assets {
		converted.Assets = append(converted.Assets, Asset{ID: asset.GetID(), Name: asset.GetName()})
	}
	return converted
}
