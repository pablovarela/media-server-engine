package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	repository = "pablovarela/media-server-engine"
	apiURL     = "https://api.github.com/repos/" + repository
)

type Asset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Client struct {
	tokens TokenSource
	token  string
	http   *http.Client
}

func NewClient(tokens TokenSource, httpClient *http.Client) *Client {
	return &Client{tokens: tokens, http: httpClient}
}

func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	var all []Release
	err := c.get(ctx, apiURL+"/releases?per_page=100", "application/vnd.github+json", func(body io.Reader) error {
		return json.NewDecoder(body).Decode(&all)
	})
	if err != nil {
		return nil, fmt.Errorf("list the releases of %s: %w", repository, err)
	}
	var published []Release
	for _, release := range all {
		if !release.Draft && !release.Prerelease {
			published = append(published, release)
		}
	}
	return published, nil
}

func (c *Client) Download(ctx context.Context, assetID int64, w io.Writer) error {
	err := c.get(ctx, fmt.Sprintf("%s/releases/assets/%d", apiURL, assetID), "application/octet-stream", func(body io.Reader) error {
		_, err := io.Copy(w, body)
		return err
	})
	if err != nil {
		return fmt.Errorf("download asset %d: %w", assetID, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, url, accept string, read func(io.Reader) error) error {
	if c.token == "" {
		token, err := c.tokens.Token(ctx)
		if err != nil {
			return err
		}
		c.token = token
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", accept)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered %s: check the token can read %s", response.Status, repository)
	}
	return read(response.Body)
}
