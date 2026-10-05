package homepage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/version"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHealthchecksSlugs(t *testing.T) {
	type Given struct {
		status int
		body   string
	}
	type Then struct {
		slugs map[string]bool
		err   string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the slugs":        {Given: Given{status: 200, body: `{"checks":[{"slug":"gorgon-backup"},{"slug":"gorgon-update"}]}`}, Then: Then{slugs: map[string]bool{"gorgon-backup": true, "gorgon-update": true}}},
		"no checks listed": {Given: Given{status: 200, body: `{"error":"x"}`}, Then: Then{err: "healthchecks listed no checks"}},
		"refused":          {Given: Given{status: 401, body: `{}`}, Then: Then{err: "healthchecks answered 401 Unauthorized"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "https://healthchecks.io/api/v3/checks/", r.URL.String())
				assert.Equal(t, "read-key", r.Header.Get("X-Api-Key"))
				return &http.Response{StatusCode: tt.Given.status, Status: map[int]string{200: "200 OK", 401: "401 Unauthorized"}[tt.Given.status], Body: io.NopCloser(strings.NewReader(tt.Given.body))}, nil
			})}

			slugs, err := Healthchecks{Client: client, URL: ChecksURL}.Slugs(context.Background(), "read-key")

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.slugs, slugs)
		})
	}
}

func TestEngineURL(t *testing.T) {
	tests := map[string]struct {
		Given struct{ build version.Build }
		Then  struct{ url string }
	}{
		"release":           {Given: struct{ build version.Build }{version.Build{Version: "v0.9.1", Commit: "513f464"}}, Then: struct{ url string }{"https://github.com/pablovarela/media-server-engine/releases/tag/v0.9.1"}},
		"dev with a commit": {Given: struct{ build version.Build }{version.Build{Version: "dev", Commit: "513f464-dirty"}}, Then: struct{ url string }{"https://github.com/pablovarela/media-server-engine/commit/513f464"}},
		"snapshot":          {Given: struct{ build version.Build }{version.Build{Version: "v0.9.2-SNAPSHOT-abc1234", Commit: "abc1234"}}, Then: struct{ url string }{"https://github.com/pablovarela/media-server-engine/commit/abc1234"}},
		"dev without":       {Given: struct{ build version.Build }{version.Build{Version: "dev"}}, Then: struct{ url string }{""}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.Then.url, EngineURL(tt.Given.build))
		})
	}
}
