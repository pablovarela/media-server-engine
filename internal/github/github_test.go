package github

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type exchange struct {
	url           string
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
	next := 0
	t.Cleanup(func() { assert.Equal(t, len(exchanges), next, "requests made") })
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Less(t, next, len(exchanges), "unexpected request to %s", r.URL)
		want := exchanges[next]
		next++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, want.url, r.URL.String())
		assert.Equal(t, want.authorization, r.Header.Get("Authorization"))
		assert.Equal(t, want.accept, r.Header.Get("Accept"))
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
	tokens := TokenSource{
		Getenv:  func(string) string { return "test-token" },
		GHToken: func(context.Context) ([]byte, error) { return nil, nil },
	}
	return NewClient(tokens, &http.Client{Transport: transport})
}

func TestReleases(t *testing.T) {
	releasesURL := "https://api.github.com/repos/pablovarela/media-server-engine/releases?per_page=100"
	type Given struct {
		status int
		body   string
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
		"token without access": {
			Given: Given{status: http.StatusNotFound, body: `{"message": "Not Found"}`},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 404 Not Found: check the token from GITHUB_TOKEN can read pablovarela/media-server-engine"},
		},
		"GitHub failing": {
			Given: Given{status: http.StatusInternalServerError, body: `{"message": "Server Error"}`},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 500 Internal Server Error"},
		},
		"expired token": {
			Given: Given{status: http.StatusUnauthorized, body: `{"message": "Bad credentials"}`},
			Then:  Then{err: "list the releases of pablovarela/media-server-engine: GitHub answered 401 Unauthorized: check the token from GITHUB_TOKEN can read pablovarela/media-server-engine"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := clientAnswering(t, exchange{
				url: releasesURL, authorization: "Bearer test-token", accept: "application/vnd.github+json",
				status: tt.Given.status, body: tt.Given.body,
			})

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
		"drops the token on the redirect to storage": {
			Given: Given{exchanges: []exchange{
				{url: assetURL, authorization: "Bearer test-token", accept: "application/octet-stream", status: http.StatusFound, location: storageURL},
				{url: storageURL, authorization: "", accept: "application/octet-stream", status: http.StatusOK, body: "archive bytes"},
			}},
			Then: Then{body: "archive bytes"},
		},
		"asset that is gone": {
			Given: Given{exchanges: []exchange{
				{url: assetURL, authorization: "Bearer test-token", accept: "application/octet-stream", status: http.StatusNotFound},
			}},
			Then: Then{err: "download asset 101: GitHub answered 404 Not Found: check the token from GITHUB_TOKEN can read pablovarela/media-server-engine"},
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
