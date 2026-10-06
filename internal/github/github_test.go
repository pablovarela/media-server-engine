package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

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
		return &http.Response{
			StatusCode: want.status,
			Status:     fmt.Sprintf("%d %s", want.status, http.StatusText(want.status)),
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(want.body)),
			Request:    r,
		}, nil
	})
	return NewClient(tokens, &http.Client{Transport: transport})
}

func TestReleases(t *testing.T) {
	releasesURL := "https://api.github.com/repos/pablovarela/media-server-engine/releases?per_page=100"
	type Given struct {
		withoutToken bool
		status       int
		body         string
	}
	type Then struct {
		tags []string
		err  string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"published releases only": {
			Given: Given{status: http.StatusOK, body: `[
				{"tag_name": "v0.9.0", "html_url": "https://github.com/pablovarela/media-server-engine/releases/tag/v0.9.0", "draft": true, "prerelease": false, "assets": []},
				{"tag_name": "v0.8.1", "html_url": "https://github.com/pablovarela/media-server-engine/releases/tag/v0.8.1", "draft": false, "prerelease": false, "assets": [{"id": 101, "name": "mse_linux_arm64.tar.gz"}]},
				{"tag_name": "v0.9.0-rc.1", "html_url": "https://github.com/pablovarela/media-server-engine/releases/tag/v0.9.0-rc.1", "draft": false, "prerelease": true, "assets": []}
			]`},
			Then: Then{tags: []string{"v0.8.1"}},
		},
		"without a token": {
			Given: Given{withoutToken: true, status: http.StatusOK, body: `[
				{"tag_name": "v0.8.1", "html_url": "https://github.com/pablovarela/media-server-engine/releases/tag/v0.8.1", "assets": [{"id": 101, "name": "mse_linux_arm64.tar.gz"}]}
			]`},
			Then: Then{tags: []string{"v0.8.1"}},
		},
		"repository not found": {
			Given: Given{status: http.StatusNotFound, body: `{"message": "Not Found"}`},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 404 Not Found"},
		},
		"GitHub failing": {
			Given: Given{status: http.StatusInternalServerError, body: `{"message": "Server Error"}`},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 500 Internal Server Error"},
		},
		"rate limited": {
			Given: Given{status: http.StatusForbidden, body: `{"message": "API rate limit exceeded for 192.0.2.1."}`},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 403 Forbidden: API rate limit exceeded for 192.0.2.1."},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			answer := exchange{url: releasesURL, accept: "application/vnd.github.v3+json", status: tt.Given.status, body: tt.Given.body}
			answering := clientAnswering
			if tt.Given.withoutToken {
				answering = clientWithoutToken
			}
			client := answering(t, answer)

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
			assert.Equal(t, []Asset{{ID: 101, Name: "mse_linux_arm64.tar.gz"}}, releases[0].Assets)
		})
	}
}

func TestDownload(t *testing.T) {
	assetURL := "https://api.github.com/repos/pablovarela/media-server-engine/releases/assets/101"
	storageURL := "https://objects.githubusercontent.com/release-asset/101"
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
		"follows the redirect to storage, with no token": {
			Given: Given{exchanges: []exchange{
				{url: assetURL, accept: "application/octet-stream", status: http.StatusFound, location: storageURL},
				{url: storageURL, authorization: "", accept: "application/octet-stream", status: http.StatusOK, body: "archive bytes"},
			}},
			Then: Then{body: "archive bytes"},
		},
		"asset that is gone": {
			Given: Given{exchanges: []exchange{
				{url: assetURL, accept: "application/octet-stream", status: http.StatusNotFound},
			}},
			Then: Then{err: "download asset 101: GitHub answered 404 Not Found"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := clientAnswering(t, tt.Given.exchanges...)
			var got bytes.Buffer

			err := client.Download(context.Background(), 101, &got)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.body, got.String())
		})
	}
}
