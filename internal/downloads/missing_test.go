package downloads

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

type command struct {
	url    string
	body   string
	status int
}

func commanding(t *testing.T, commands ...command) *http.Client {
	t.Helper()
	next := 0
	t.Cleanup(func() { assert.Equal(t, len(commands), next, "commands sent") })
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Less(t, next, len(commands), "unexpected %s %s", r.Method, r.URL)
		want := commands[next]
		next++
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, want.url, r.URL.String())
		assert.NotEmpty(t, r.Header.Get("X-Api-Key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, want.body, string(body))
		if want.status == 0 {
			return nil, errors.New("connection refused")
		}
		return &http.Response{StatusCode: want.status, Status: fmt.Sprint(want.status), Body: io.NopCloser(strings.NewReader(`{"id":1}`))}, nil
	})}
}

func TestSearchMissing(t *testing.T) {
	sonarr := command{url: "http://localhost:8989/api/v3/command", body: `{"name":"MissingEpisodeSearch","monitored":true}`, status: http.StatusCreated}
	radarr := command{url: "http://localhost:7878/api/v3/command", body: `{"name":"MissingMoviesSearch"}`, status: http.StatusCreated}
	type Given struct {
		radarr   bool
		commands []command
	}
	type Then struct {
		out string
		err string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"both apps search": {
			Given: Given{radarr: true, commands: []command{sonarr, radarr}},
			Then:  Then{out: "sonarr: searching for missing episodes\nradarr: searching for missing movies\n"},
		},
		"an app without a key is skipped": {
			Given: Given{commands: []command{sonarr}},
			Then:  Then{out: "sonarr: searching for missing episodes\nradarr: not set up, skipped\n"},
		},
		"an app that isn't answering fails the run": {
			Given: Given{radarr: true, commands: []command{{url: sonarr.url, body: sonarr.body}, radarr}},
			Then:  Then{out: "radarr: searching for missing movies\n", err: "sonarr: could not start the search: connection refused"},
		},
		"an app that refuses fails the run": {
			Given: Given{radarr: true, commands: []command{sonarr, {url: radarr.url, body: radarr.body, status: http.StatusUnauthorized}}},
			Then:  Then{out: "sonarr: searching for missing episodes\n", err: "radarr: could not start the search: Radarr answered 401 Unauthorized"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer

			err := SearchMissing(context.Background(), commanding(t, tt.Given.commands...), withKeys(t, true, tt.Given.radarr), &out)

			assert.Equal(t, tt.Then.out, out.String())
			if tt.Then.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.Then.err)
		})
	}
}
