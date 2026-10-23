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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/runs"
)

func TestRemoveExecutableDownloadsWithoutKeys(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t), Host: thisHost})
	var stdout bytes.Buffer
	root.SetOut(&stdout)

	code := run(context.Background(), root, []string{"clean-downloads"})

	assert.Equal(t, 0, code)
	assert.Equal(t, "sonarr: queue not reachable, skipped\nradarr: queue not reachable, skipped\n", stdout.String())
	run, recorded, err := runs.Read(filepath.Join(home, ".local", "state", "mse", "gorgon", "runs"), "clean-downloads")
	require.NoError(t, err)
	assert.True(t, recorded, "the run is recorded for mse status")
	assert.False(t, run.Ended.IsZero())
	assert.False(t, run.Failed)
}

func TestCleanDownloadsStillCleansWhenItsRunCannotBeRecorded(t *testing.T) {
	getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
	state := filepath.Join(home, ".local", "state", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(state, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(state, "runs"), nil, 0o644))
	root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t), Host: thisHost})
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"clean-downloads"})

	assert.Equal(t, 0, code)
	assert.Contains(t, stdout.String(), "sonarr: queue not reachable, skipped\n")
	assert.Contains(t, stderr.String(), "could not record the clean-downloads run for mse status")
}

func TestCleanDownloadsReportsItsRunToHealthchecks(t *testing.T) {
	tests := map[string]struct {
		removal int
		pings   []string
		failed  bool
	}{
		"a clean run": {removal: http.StatusOK, pings: []string{"/ping-key/mse-gorgon-clean-downloads/start", "/ping-key/mse-gorgon-clean-downloads"}},
		"a download that can't be removed": {removal: http.StatusInternalServerError, failed: true,
			pings: []string{"/ping-key/mse-gorgon-clean-downloads/start", "/ping-key/mse-gorgon-clean-downloads/fail"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			getenv, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\n"})
			state := filepath.Join(home, ".local", "state", "mse", "gorgon")
			require.NoError(t, os.MkdirAll(filepath.Join(state, ".secrets"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(state, ".secrets", "healthchecks.env"), []byte("HEALTHCHECKS_PING_KEY=ping-key\n"), 0o600))
			sonarr := filepath.Join(home, ".local", "share", "mse", "gorgon", "volumes", "sonarr", "data")
			require.NoError(t, os.MkdirAll(sonarr, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(sonarr, "config.xml"), []byte("<Config><ApiKey>sonarr-key</ApiKey></Config>"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(home, ".local", "share", "mse", "gorgon", ".backup-main"), nil, 0o644))
			var pings []string
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.Host == "hc-ping.com":
					pings = append(pings, r.URL.Path)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("OK"))}, nil
				case r.URL.Host == "localhost:8989" && r.Method == http.MethodGet:
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
						`{"totalRecords":1,"records":[{"id":7,"title":"Show.S01E01","statusMessages":[{"messages":["Found executable file"]}]}]}`))}, nil
				case r.URL.Host == "localhost:8989" && r.Method == http.MethodDelete:
					return &http.Response{StatusCode: tt.removal, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return nil, errors.New("not reachable")
			})}
			root := NewRootCommand(Dependencies{Environment: getenv, Home: home, Update: newMockUpdater(t), Host: thisHost, HTTP: client})
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)

			run(context.Background(), root, []string{"clean-downloads"})

			assert.Equal(t, tt.pings, pings)
			record, recorded, err := runs.Read(filepath.Join(state, "runs"), "clean-downloads")
			require.NoError(t, err)
			require.True(t, recorded)
			assert.Equal(t, tt.failed, record.Failed)
		})
	}
}

var thisHost = installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon.local", nil }}
