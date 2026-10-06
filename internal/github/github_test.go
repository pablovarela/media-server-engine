package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type exchange struct {
	method        string
	url           string
	request       string
	authorization string
	accept        string
	status        int
	body          string
	location      string
	headers       map[string]string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientAnswering(t *testing.T, exchanges ...exchange) *Client {
	t.Helper()
	tokens := TokenSource{
		Getenv:  func(string) string { return "test-token" },
		GHToken: func(context.Context) ([]byte, error) { return nil, nil },
	}
	return clientWith(t, tokens, exchanges...)
}

func clientWithoutToken(t *testing.T, exchanges ...exchange) *Client {
	t.Helper()
	tokens := TokenSource{
		Getenv:  func(string) string { return "" },
		GHToken: func(context.Context) ([]byte, error) { return nil, errors.New("not logged in") },
	}
	return clientWith(t, tokens, exchanges...)
}

func clientWith(t *testing.T, tokens TokenSource, exchanges ...exchange) *Client {
	t.Helper()
	next := 0
	t.Cleanup(func() { assert.Equal(t, len(exchanges), next, "requests made") })
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Less(t, next, len(exchanges), "unexpected request to %s", r.URL)
		want := exchanges[next]
		next++
		method := want.method
		if method == "" {
			method = http.MethodGet
		}
		assert.Equal(t, method, r.Method)
		if want.request != "" {
			sent, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, want.request, string(sent))
		}
		assert.Equal(t, want.url, r.URL.String())
		assert.Equal(t, want.authorization, r.Header.Get("Authorization"))
		if want.accept != "" {
			assert.Equal(t, want.accept, r.Header.Get("Accept"))
		}
		header := http.Header{}
		if want.location != "" {
			header.Set("Location", want.location)
		}
		for key, value := range want.headers {
			header.Set(key, value)
		}
		return &http.Response{
			StatusCode: want.status,
			Status:     fmt.Sprintf("%d %s", want.status, http.StatusText(want.status)),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(want.body)),
			Request:    r,
		}, nil
	})
	client, err := NewClient(tokens, &http.Client{Transport: transport})
	require.NoError(t, err)
	return client
}

var rateLimitReset = time.Date(2026, 10, 6, 18, 30, 0, 0, time.UTC)

func rateLimited() exchange {
	return exchange{
		url: releasesURL, accept: "application/vnd.github.v3+json", status: http.StatusForbidden,
		body: `{"message": "API rate limit exceeded for 192.0.2.1."}`,
		headers: map[string]string{
			"X-RateLimit-Limit":     "60",
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":     strconv.FormatInt(rateLimitReset.Unix(), 10),
		},
	}
}

const releasesURL = "https://api.github.com/repos/pablovarela/media-server-engine/releases?per_page=100"

const oneRelease = `[
	{"tag_name": "v0.8.1", "html_url": "https://github.com/pablovarela/media-server-engine/releases/tag/v0.8.1", "draft": false, "prerelease": false,
	 "assets": [{"id": 101, "name": "mse_linux_arm64.tar.gz", "browser_download_url": "https://github.com/pablovarela/media-server-engine/releases/download/v0.8.1/mse_linux_arm64.tar.gz"}]}
]`

func TestReleases(t *testing.T) {
	listed := exchange{url: releasesURL, accept: "application/vnd.github.v3+json", status: http.StatusOK, body: oneRelease}
	type Given struct {
		withoutToken bool
		exchanges    []exchange
	}
	type Then struct {
		tags []string
		err  string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"published releases only, read without the token": {
			Given: Given{exchanges: []exchange{{url: releasesURL, accept: "application/vnd.github.v3+json", status: http.StatusOK, body: `[
				{"tag_name": "v0.9.0", "draft": true, "prerelease": false, "assets": []},
				{"tag_name": "v0.8.1", "html_url": "https://github.com/pablovarela/media-server-engine/releases/tag/v0.8.1", "draft": false, "prerelease": false,
				 "assets": [{"id": 101, "name": "mse_linux_arm64.tar.gz", "browser_download_url": "https://github.com/pablovarela/media-server-engine/releases/download/v0.8.1/mse_linux_arm64.tar.gz"}]},
				{"tag_name": "v0.9.0-rc.1", "draft": false, "prerelease": true, "assets": []}
			]`}}},
			Then: Then{tags: []string{"v0.8.1"}},
		},
		"without a token": {
			Given: Given{withoutToken: true, exchanges: []exchange{listed}},
			Then:  Then{tags: []string{"v0.8.1"}},
		},
		"rate limited, then read with the token": {
			Given: Given{exchanges: []exchange{rateLimited(), {url: releasesURL, authorization: "Bearer test-token", status: http.StatusOK, body: oneRelease}}},
			Then:  Then{tags: []string{"v0.8.1"}},
		},
		"rate limited, with no token to fall back on": {
			Given: Given{withoutToken: true, exchanges: []exchange{rateLimited()}},
			Then: Then{err: "list the releases of pablovarela/media-server-engine: GitHub's limit of 60 requests an hour from this address is used up until " +
				rateLimitReset.Local().Format("15:04") + "; with gh logged in, mse uses its token instead"},
		},
		"repository not found": {
			Given: Given{exchanges: []exchange{{url: releasesURL, status: http.StatusNotFound, body: `{"message": "Not Found"}`}}},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 404 Not Found"},
		},
		"GitHub failing": {
			Given: Given{exchanges: []exchange{{url: releasesURL, status: http.StatusInternalServerError, body: `{"message": "Server Error"}`}}},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 500 Internal Server Error"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			answering := clientAnswering
			if tt.Given.withoutToken {
				answering = clientWithoutToken
			}
			client := answering(t, tt.Given.exchanges...)

			releases, err := client.Releases(context.Background())

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			var tags []string
			for _, r := range releases {
				tags = append(tags, r.Tag)
			}
			assert.Equal(t, tt.Then.tags, tags)
			assert.Equal(t, []Asset{{ID: 101, Name: "mse_linux_arm64.tar.gz", URL: "https://github.com/pablovarela/media-server-engine/releases/download/v0.8.1/mse_linux_arm64.tar.gz"}}, releases[0].Assets)
		})
	}
}

func TestDownload(t *testing.T) {
	asset := Asset{ID: 101, Name: "mse_linux_arm64.tar.gz", URL: "https://github.com/pablovarela/media-server-engine/releases/download/v0.8.1/mse_linux_arm64.tar.gz"}
	storageURL := "https://release-assets.githubusercontent.com/github-production-release-asset/101"
	type Given struct {
		exchanges []exchange
	}
	type Then struct {
		body string
		err  string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"from the release's download URL, with no token": {
			Given: Given{exchanges: []exchange{
				{url: asset.URL, status: http.StatusFound, location: storageURL},
				{url: storageURL, status: http.StatusOK, body: "archive bytes"},
			}},
			Then: Then{body: "archive bytes"},
		},
		"asset that is gone": {
			Given: Given{exchanges: []exchange{{url: asset.URL, status: http.StatusNotFound}}},
			Then:  Then{err: "download mse_linux_arm64.tar.gz: GitHub answered 404 Not Found"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := clientAnswering(t, tt.Given.exchanges...)
			var got bytes.Buffer

			err := client.Download(context.Background(), asset, &got)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.body, got.String())
		})
	}
}
