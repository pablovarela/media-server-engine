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
	"time"

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
	assert.DirExists(t, filepath.Join(cache, "0.18.0"), "the most recent other version stays for an older mse")
	assert.Equal(t, 1, *requests)
	assert.Equal(t, "Downloading restic 0.19.1... done.\n", out.String())
	parts, _ := filepath.Glob(filepath.Join(cache, "0.19.1", "*.part"))
	assert.Empty(t, parts, "no temporary file left")
	assert.FileExists(t, path+".sha256")
}

func TestBinaryUsesTheCachedRestic(t *testing.T) {
	cache := t.TempDir()
	path := filepath.Join(cache, "0.19.1", "restic")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("cached"), 0o755))
	require.NoError(t, os.WriteFile(path+".sha256", []byte(hexSum([]byte("cached"))+"\n"), 0o644))
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
	folder := filepath.Join(cache, "0.19.1")
	require.NoError(t, os.MkdirAll(folder, 0o755))
	others := []string{"restic.part", "restic-1.part", "restic-12345.part"}
	for _, name := range others {
		require.NoError(t, os.WriteFile(filepath.Join(folder, name), []byte("another process's half-written file"), 0o644))
	}
	archive, sum := testArchive(t)
	fetch, _, _ := fetchServing(t, cache, http.StatusOK, archive, sum)

	_, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	for _, name := range others {
		content, err := os.ReadFile(filepath.Join(folder, name))
		require.NoError(t, err, name)
		assert.Equal(t, "another process's half-written file", string(content), name)
	}
}

func TestBinaryKeepsTheMostRecentOtherVersion(t *testing.T) {
	cache := t.TempDir()
	old, older := filepath.Join(cache, "0.18.0"), filepath.Join(cache, "0.17.0")
	for _, folder := range []string{older, old} {
		require.NoError(t, os.MkdirAll(folder, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(folder, "restic"), []byte("old"), 0o755))
	}
	require.NoError(t, os.Chtimes(older, time.Now().Add(-40*24*time.Hour), time.Now().Add(-40*24*time.Hour)))
	require.NoError(t, os.Chtimes(old, time.Now().Add(-40*24*time.Hour), time.Now().Add(-40*24*time.Hour)))
	archive, sum := testArchive(t)
	fetch, _, _ := fetchServing(t, cache, http.StatusOK, archive, sum)

	_, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(old, "restic"), "an older mse may still be running it")
	assert.NoDirExists(t, older)
}

func TestBinaryRemovesStaleTemporaryFiles(t *testing.T) {
	cache := t.TempDir()
	stale := filepath.Join(cache, "0.19.1", "restic-999.part")
	require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o755))
	require.NoError(t, os.WriteFile(stale, []byte("killed mid-download"), 0o644))
	require.NoError(t, os.Chtimes(stale, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)))
	archive, sum := testArchive(t)
	fetch, _, _ := fetchServing(t, cache, http.StatusOK, archive, sum)

	_, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	assert.NoFileExists(t, stale)
}

func TestBinaryLeavesAFolderAnotherProcessFilled(t *testing.T) {
	cache := t.TempDir()
	installed := filepath.Join(cache, "0.19.1", "restic")
	fetch, _, _ := fetchServing(t, cache, http.StatusNotFound, nil, "x")
	fetch.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		require.NoError(t, os.MkdirAll(filepath.Dir(installed), 0o755))
		require.NoError(t, os.WriteFile(installed, []byte("installed by another process"), 0o755))
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}

	_, err := Binary(context.Background(), fetch)

	require.Error(t, err)
	assert.FileExists(t, installed)
}

func TestBinaryRefusesAnOversizedDownload(t *testing.T) {
	cache := t.TempDir()
	fetch, _, _ := fetchServing(t, cache, http.StatusOK, bytes.Repeat([]byte("x"), 2048), "x")
	limit := maxArchive
	maxArchive = 1024
	t.Cleanup(func() { maxArchive = limit })

	_, err := Binary(context.Background(), fetch)

	assert.EqualError(t, err, "could not download restic 0.19.1 from github.com/restic/restic (the archive is larger than 1024 bytes)")
	assertNothingInstalled(t, cache)
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
			assertNothingInstalled(t, cache)
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
	assertNothingInstalled(t, cache)
}

func hexSum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func assertNothingInstalled(t *testing.T, cache string) {
	t.Helper()
	folder := filepath.Join(cache, "0.19.1")
	assert.NoFileExists(t, filepath.Join(folder, "restic"))
	parts, _ := filepath.Glob(filepath.Join(folder, "*.part"))
	assert.Empty(t, parts, "no temporary file left")
}

func TestBinaryKeepsAFolderAnotherProcessIsInstallingInto(t *testing.T) {
	cache := t.TempDir()
	month := time.Now().Add(-40 * 24 * time.Hour)
	folders := map[string]time.Time{"0.18.0": month, "0.17.0": time.Now(), "0.16.0": month}
	for name, modified := range folders {
		folder := filepath.Join(cache, name)
		require.NoError(t, os.MkdirAll(folder, 0o755))
		require.NoError(t, os.Chtimes(folder, modified, modified))
	}
	archive, sum := testArchive(t)
	fetch, _, _ := fetchServing(t, cache, http.StatusOK, archive, sum)

	_, err := Binary(context.Background(), fetch)

	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(cache, "0.18.0"), "the newest other version")
	assert.DirExists(t, filepath.Join(cache, "0.17.0"), "touched within the month, maybe being installed")
	assert.NoDirExists(t, filepath.Join(cache, "0.16.0"))
}

func TestBinaryRepairsADamagedCache(t *testing.T) {
	tests := map[string]func(path string){
		"truncated":   func(path string) { require.NoError(t, os.WriteFile(path, []byte("trunc"), 0o755)) },
		"no checksum": func(path string) { require.NoError(t, os.Remove(path+".sha256")) },
	}
	for name, damage := range tests {
		t.Run(name, func(t *testing.T) {
			cache := t.TempDir()
			archive, sum := testArchive(t)
			fetch, requests, _ := fetchServing(t, cache, http.StatusOK, archive, sum)
			path, err := Binary(context.Background(), fetch)
			require.NoError(t, err)
			damage(path)

			_, err = Binary(context.Background(), fetch)

			require.NoError(t, err)
			assert.Equal(t, 2, *requests, "downloaded again")
			content, _ := os.ReadFile(path)
			assert.Equal(t, "#!/bin/sh\necho \"restic 0.19.1\"\n", string(content))
		})
	}
}
