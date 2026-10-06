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
	URL  string
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

func NewClient(tokens TokenSource, httpClient *http.Client) (*Client, error) {
	public, err := gh.NewClient(gh.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{tokens: tokens, http: httpClient, public: public}, nil
}

func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	all, err := listReleases(ctx, c.public)
	if _, limited := errors.AsType[*gh.RateLimitError](err); limited {
		if api, tokenErr := c.authenticated(ctx); tokenErr == nil {
			all, err = listReleases(ctx, api)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("list the releases of %s: %w", repository, c.explain(err, publicly))
	}
	var published []Release
	for _, release := range all {
		if !release.GetDraft() && !release.GetPrerelease() {
			published = append(published, releaseOf(release))
		}
	}
	return published, nil
}

func listReleases(ctx context.Context, api *gh.Client) ([]*gh.RepositoryRelease, error) {
	all, _, err := api.Repositories.ListReleases(ctx, owner, name, &gh.ListOptions{PerPage: 100})
	return all, err
}

func (c *Client) Download(ctx context.Context, asset Asset, w io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: GitHub answered %s", asset.Name, response.Status)
	}
	if _, err := io.Copy(w, response.Body); err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
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

const publicly = ""

func (c *Client) explain(err error, can string) error {
	if limited, ok := errors.AsType[*gh.RateLimitError](err); ok {
		return fmt.Errorf("GitHub's limit of %d requests an hour from this address is used up until %s; with gh logged in, mse uses its token instead",
			limited.Rate.Limit, limited.Rate.Reset.Local().Format("15:04"))
	}
	answer, ok := errors.AsType[*gh.ErrorResponse](err)
	if !ok || answer.Response == nil {
		return err
	}
	said := "GitHub answered " + answer.Response.Status
	reason := reasonOf(answer)
	if strings.Contains(answer.Response.Status, reason) {
		reason = ""
	}
	if reason != "" {
		said += ": " + reason
	}
	switch answer.Response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		if can == publicly {
			return errors.New(said)
		}
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
		converted.Assets = append(converted.Assets, Asset{ID: asset.GetID(), Name: asset.GetName(), URL: asset.GetBrowserDownloadURL()})
	}
	return converted
}
