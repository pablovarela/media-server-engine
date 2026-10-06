package restic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/report"
)

const archiveURL = "https://github.com/restic/restic/releases/download/v0.19.1/restic_0.19.1_linux_arm64.bz2"

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testArchive(t *testing.T) ([]byte, string) {
	t.Helper()
	archive, err := os.ReadFile("testdata/restic.bz2")
	require.NoError(t, err)
	sum := sha256.Sum256(archive)
	return archive, hex.EncodeToString(sum[:])
}

func fetchServing(t *testing.T, cache string, status int, body []byte, pin string) (Fetch, *int, *bytes.Buffer) {
	t.Helper()
	requests := 0
	var out bytes.Buffer
	return Fetch{
		Release: Release{Version: "0.19.1", SHA256: map[string]string{"linux_arm64": pin}},
		Cache:   cache, GOOS: "linux", GOARCH: "arm64",
		HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			requests++
			assert.Equal(t, archiveURL, r.URL.String())
			assert.Empty(t, r.Header.Get("Authorization"))
			return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
		})},
		Report: report.New(&out, &out, nil),
	}, &requests, &out
}

func TestBinaryDownloadsVerifiesAndPlacesRestic(t *testing.T) {
	cache := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cache, "0.18.0"), 0o755))
	archive, sum := testArchive(t)
	fetch, requests, out := fetchServing(t, cache, http.StatusOK, archive, sum)

	path, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cache, "0.19.1", "restic"), path)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\necho \"restic 0.19.1\"\n", string(content))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	assert.NoDirExists(t, filepath.Join(cache, "0.18.0"))
	assert.Equal(t, 1, *requests)
	assert.Equal(t, "Downloading restic 0.19.1... done.\n", out.String())
	entries, _ := os.ReadDir(filepath.Join(cache, "0.19.1"))
	assert.Len(t, entries, 1, "no temporary file left")
}

func TestBinaryUsesTheCachedRestic(t *testing.T) {
	cache := t.TempDir()
	path := filepath.Join(cache, "0.19.1", "restic")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("cached"), 0o755))
	fetch, requests, out := fetchServing(t, cache, http.StatusOK, nil, "unused")

	got, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	assert.Equal(t, path, got)
	assert.Zero(t, *requests)
	assert.Empty(t, out.String())
}

func TestBinaryDownloadsIntoAnEmptyVersionFolder(t *testing.T) {
	cache := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cache, "0.19.1"), 0o755))
	archive, sum := testArchive(t)
	fetch, requests, _ := fetchServing(t, cache, http.StatusOK, archive, sum)

	path, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	assert.FileExists(t, path)
	assert.Equal(t, 1, *requests)
}

func TestBinaryUsesItsOwnTemporaryFile(t *testing.T) {
	cache := t.TempDir()
	stale := filepath.Join(cache, "0.19.1", "restic.download")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
	require.NoError(t, os.WriteFile(stale, []byte("another process's half-written file"), 0o644))
	archive, sum := testArchive(t)
	fetch, _, _ := fetchServing(t, cache, http.StatusOK, archive, sum)

	_, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	content, err := os.ReadFile(stale)
	require.NoError(t, err)
	assert.Equal(t, "another process's half-written file", string(content), "the other process's file is untouched")
}

func TestBinaryFailures(t *testing.T) {
	archive, sum := testArchive(t)
	tests := map[string]struct {
		status int
		body   []byte
		pin    string
		goarch string
		err    string
	}{
		"checksum": {status: http.StatusOK, body: archive, pin: strings.Repeat("0", 64),
			err: "restic_0.19.1_linux_arm64.bz2 doesn't match its pinned checksum; nothing was installed"},
		"not found": {status: http.StatusNotFound, body: []byte("Not Found"), pin: sum,
			err: "could not download restic 0.19.1 from github.com/restic/restic (GitHub answered 404 Not Found)"},
		"not bzip2": {status: http.StatusOK, body: []byte("plain text"), pin: hexSum([]byte("plain text")),
			err: "restic_0.19.1_linux_arm64.bz2 isn't a bzip2 archive (bzip2 data invalid: bad magic value)"},
		"no pin": {goarch: "386", pin: sum, err: "this mse has no pinned restic for linux/386"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			cache := t.TempDir()
			fetch, _, _ := fetchServing(t, cache, tt.status, tt.body, tt.pin)
			if tt.goarch != "" {
				fetch.GOARCH = tt.goarch
			}

			_, err := Binary(context.Background(), fetch)

			assert.EqualError(t, err, tt.err)
			assert.NoDirExists(t, filepath.Join(cache, "0.19.1"))
		})
	}
}

func TestBinaryWhenTheNetworkFails(t *testing.T) {
	cache := t.TempDir()
	fetch, _, _ := fetchServing(t, cache, 0, nil, "x")
	fetch.HTTP = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("no route to host") })}

	_, err := Binary(context.Background(), fetch)

	assert.ErrorContains(t, err, "could not download restic 0.19.1 from github.com/restic/restic (")
	assert.ErrorContains(t, err, "no route to host")
	assert.NoDirExists(t, filepath.Join(cache, "0.19.1"))
}

func hexSum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
