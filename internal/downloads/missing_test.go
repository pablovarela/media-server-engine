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
	"time"

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
		out    string
		err    string
		pauses int
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
		"an app still starting is asked again": {
			Given: Given{radarr: true, commands: []command{{url: sonarr.url, body: sonarr.body}, {url: sonarr.url, body: sonarr.body, status: http.StatusServiceUnavailable}, sonarr, radarr}},
			Then:  Then{out: "sonarr: searching for missing episodes\nradarr: searching for missing movies\n", pauses: 2},
		},
		"an app that never answers fails the run": {
			Given: Given{radarr: true, commands: append(repeat(command{url: sonarr.url, body: sonarr.body}, 31), radarr)},
			Then: Then{out: "radarr: searching for missing movies\n", pauses: 30,
				err: `sonarr: could not start the search: Post "http://localhost:8989/api/v3/command": connection refused`},
		},
		"an app that refuses fails the run": {
			Given: Given{radarr: true, commands: []command{sonarr, {url: radarr.url, body: radarr.body, status: http.StatusUnauthorized}}},
			Then:  Then{out: "sonarr: searching for missing episodes\n", err: "radarr: could not start the search: Radarr answered 401 Unauthorized"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			var paused []time.Duration
			pause := func(_ context.Context, d time.Duration) error { paused = append(paused, d); return nil }

			err := SearchMissing(context.Background(), commanding(t, tt.Given.commands...), withKeys(t, true, tt.Given.radarr), &out, pause)

			assert.Equal(t, tt.Then.out, out.String())
			assert.Len(t, paused, tt.Then.pauses)
			for _, p := range paused {
				assert.Equal(t, 10*time.Second, p)
			}
			if tt.Then.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.Then.err)
		})
	}
}

func repeat(c command, n int) []command {
	commands := make([]command, n)
	for i := range commands {
		commands[i] = c
	}
	return commands
}
