package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/runs"
)

func TestSearchMissing(t *testing.T) {
	start, success, failure := "/ping-key/mse-home-search-missing/start", "/ping-key/mse-home-search-missing", "/ping-key/mse-home-search-missing/fail"
	type Given struct {
		radarrDown     bool
		restorePending bool
	}
	type Then struct {
		code   int
		stdout string
		stderr string
		pings  []string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"both searches start": {
			Then: Then{stdout: "sonarr: searching for missing episodes\nradarr: searching for missing movies\n", pings: []string{start, success}},
		},
		"Radarr refuses": {
			Given: Given{radarrDown: true},
			Then: Then{code: 1, stdout: "sonarr: searching for missing episodes\n", stderr: "radarr: could not start the search: Radarr answered 401 Unauthorized",
				pings: []string{start, failure}},
		},
		"the media is still to be restored": {
			Given: Given{restorePending: true},
			Then: Then{stdout: "skipped: home's media is still to be restored, and Sonarr and Radarr would download all of it again\n",
				pings: []string{start, success}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"home": "INSTALLATION_NAME=home\n"})
			state := filepath.Join(home, ".local", "state", "mse", "home")
			require.NoError(t, os.MkdirAll(filepath.Join(state, ".secrets"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(state, ".secrets", "healthchecks.env"), []byte("HEALTHCHECKS_PING_KEY=ping-key\n"), 0o600))
			data := filepath.Join(home, ".local", "share", "mse", "home")
			for app, config := range map[string]string{"sonarr": "volumes/sonarr/data/config.xml", "radarr": "volumes/radarr/config/config.xml"} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(data, config)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(data, config), []byte("<Config><ApiKey>"+app+"-key</ApiKey></Config>"), 0o644))
			}
			require.NoError(t, os.WriteFile(filepath.Join(data, ".backup-main"), nil, 0o644))
			if tt.Given.restorePending {
				require.NoError(t, os.WriteFile(filepath.Join(data, ".media-restore-pending"), nil, 0o644))
			}
			var pings []string
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.Host == "hc-ping.com":
					pings = append(pings, r.URL.Path)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("OK"))}, nil
				case r.URL.Host == "localhost:7878" && tt.Given.radarrDown:
					return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(""))}, nil
				case r.URL.Path == "/api/v3/command":
					return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"id":1}`))}, nil
				}
				return nil, errors.New("not reachable")
			})}
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t), Host: thisHost, HTTP: client,
				Pause: func(context.Context, time.Duration) error { return nil }})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, []string{"search-missing"})

			assert.Equal(t, tt.Then.code, code)
			assert.Equal(t, tt.Then.stdout, stdout.String())
			if tt.Then.stderr == "" {
				assert.Empty(t, stderr.String())
			} else {
				assert.Contains(t, stderr.String(), tt.Then.stderr)
			}
			assert.Equal(t, tt.Then.pings, pings)
			record, recorded, err := runs.Read(filepath.Join(state, "runs"), "search-missing")
			require.NoError(t, err)
			require.True(t, recorded, "the run is recorded for mse status")
			assert.Equal(t, tt.Then.code != 0, record.Failed)
		})
	}
}
