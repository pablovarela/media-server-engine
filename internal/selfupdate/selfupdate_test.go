package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/version"
)

const (
	archiveID   = 101
	checksumsID = 102
)

func releaseOf(tag string) github.Release {
	return github.Release{
		Tag: tag,
		URL: "https://github.com/pablovarela/media-server-engine/releases/tag/" + tag,
		Assets: []github.Asset{
			{ID: archiveID, Name: ArchiveName()},
			{ID: checksumsID, Name: "checksums.txt"},
		},
	}
}

func TestUpdate(t *testing.T) {
	archive := tarGz(t, "new binary")
	sum := sha256.Sum256(archive)
	goodChecksums := hex.EncodeToString(sum[:]) + "  " + ArchiveName() + "\n"

	type Given struct {
		running   string
		releases  []github.Release
		checksums string
		version   func() (string, error)
		readOnly  bool
	}
	type When struct {
		force bool
	}
	type Then struct {
		result Result
		err    string
		binary string
	}
	answers := func(tag string) func() (string, error) {
		return func() (string, error) { return "mse " + tag + " (commit 1a2b3c4, built 2026-10-05)\n", nil }
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"newest release of the running major": {
			Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.1"), releaseOf("v0.9.0")}, checksums: goodChecksums, version: answers("v0.9.0")},
			Then:  Then{result: Result{From: "v0.8.0", To: "v0.9.0"}, binary: "new binary"},
		},
		"up to date": {
			Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.0")}},
			Then:  Then{result: Result{From: "v0.8.0"}, binary: "old binary"},
		},
		"newer major reported after updating within the running one": {
			Given: Given{running: "v1.2.0", releases: []github.Release{releaseOf("v1.2.1"), releaseOf("v2.0.0")}, checksums: goodChecksums, version: answers("v1.2.1")},
			Then: Then{
				result: Result{From: "v1.2.0", To: "v1.2.1", NewerMajor: ptr(releaseOf("v2.0.0"))},
				binary: "new binary",
			},
		},
		"force crosses to the newer major": {
			Given: Given{running: "v1.2.0", releases: []github.Release{releaseOf("v1.2.1"), releaseOf("v2.0.0")}, checksums: goodChecksums, version: answers("v2.0.0")},
			When:  When{force: true},
			Then:  Then{result: Result{From: "v1.2.0", To: "v2.0.0"}, binary: "new binary"},
		},
		"dev build": {
			Given: Given{running: "dev"},
			Then:  Then{err: ErrDevBuild.Error(), binary: "old binary"},
		},
		"no archive for this machine": {
			Given: Given{running: "v0.8.0", releases: []github.Release{{Tag: "v0.8.1", Assets: []github.Asset{{ID: checksumsID, Name: "checksums.txt"}}}}},
			Then:  Then{err: "release v0.8.1 has no " + ArchiveName(), binary: "old binary"},
		},
		"checksum mismatch": {
			Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.1")}, checksums: "0000  " + ArchiveName() + "\n"},
			Then:  Then{err: "release v0.8.1: checksum mismatch for " + ArchiveName(), binary: "old binary"},
		},
		"downloaded binary does not run": {
			Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.1")}, checksums: goodChecksums, version: func() (string, error) {
				return "", errors.New("exec format error")
			}},
			Then: Then{err: "the downloaded mse v0.8.1 does not run: exec format error", binary: "old binary"},
		},
		"downloaded binary is another version": {
			Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.1")}, checksums: goodChecksums, version: answers("v0.8.0")},
			Then:  Then{err: `the downloaded mse says "mse v0.8.0 (commit 1a2b3c4, built 2026-10-05)", not v0.8.1`, binary: "old binary"},
		},
		"unwritable directory": {
			Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.1")}, checksums: goodChecksums, readOnly: true},
			Then:  Then{err: "cannot write to <dir>", binary: "old binary"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			executable := filepath.Join(dir, "mse")
			require.NoError(t, os.WriteFile(executable, []byte("old binary"), 0o755))
			if tt.Given.readOnly {
				require.NoError(t, os.Chmod(dir, 0o555))
				t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			}
			source := newMockReleaseSource(t)
			source.EXPECT().Releases(mock.Anything).Return(tt.Given.releases, nil).Maybe()
			source.EXPECT().Download(mock.Anything, int64(archiveID), mock.Anything).RunAndReturn(writing(archive)).Maybe()
			source.EXPECT().Download(mock.Anything, int64(checksumsID), mock.Anything).RunAndReturn(writing([]byte(tt.Given.checksums))).Maybe()
			versions := newMockVersionReader(t)
			if tt.Given.version != nil {
				versions.EXPECT().Version(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, path string) (string, error) {
					assert.Equal(t, dir, filepath.Dir(path), "the new binary is tried next to the old one")
					return tt.Given.version()
				}).Maybe()
			}
			updater := New(source, func() (string, error) { return executable, nil }, versions)

			result, err := updater.Update(context.Background(), version.Build{Version: tt.Given.running}, tt.When.force, &progressLog{})

			if tt.Then.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), strings.ReplaceAll(tt.Then.err, "<dir>", dir))
			} else {
				require.NoError(t, err)
				want := tt.Then.result
				if want.To != "" {
					want.Path = executable
				}
				assert.Equal(t, want, result)
			}
			binary, err := os.ReadFile(executable)
			require.NoError(t, err)
			assert.Equal(t, tt.Then.binary, string(binary))
			info, err := os.Stat(executable)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "mse stays executable")
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, 1, "only mse is left in its directory")
		})
	}
}

func TestUpdateFollowsSymlink(t *testing.T) {
	archive := tarGz(t, "new binary")
	sum := sha256.Sum256(archive)
	dir := t.TempDir()
	target := filepath.Join(dir, "versions", "mse")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))
	link := filepath.Join(dir, "mse")
	require.NoError(t, os.Symlink(target, link))
	source := newMockReleaseSource(t)
	source.EXPECT().Releases(mock.Anything).Return([]github.Release{releaseOf("v0.8.1")}, nil)
	source.EXPECT().Download(mock.Anything, int64(archiveID), mock.Anything).RunAndReturn(writing(archive))
	source.EXPECT().Download(mock.Anything, int64(checksumsID), mock.Anything).RunAndReturn(writing([]byte(hex.EncodeToString(sum[:]) + "  " + ArchiveName() + "\n")))
	versions := newMockVersionReader(t)
	versions.EXPECT().Version(mock.Anything, mock.Anything).Return("mse v0.8.1 (commit 1a2b3c4, built 2026-10-05)\n", nil)

	_, err := New(source, func() (string, error) { return link, nil }, versions).
		Update(context.Background(), version.Build{Version: "v0.8.0"}, false, &progressLog{})

	require.NoError(t, err)
	binary, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new binary", string(binary))
	entries, err := os.ReadDir(filepath.Dir(target))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only mse is left next to the target")
	resolved, err := os.Readlink(link)
	require.NoError(t, err)
	assert.Equal(t, target, resolved)
}

func writing(content []byte) func(context.Context, int64, io.Writer) error {
	return func(_ context.Context, _ int64, w io.Writer) error {
		_, err := w.Write(content)
		return err
	}
}

func ptr(r github.Release) *github.Release { return &r }

func tarGz(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "mse", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(binary))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

type progressLog []string

func (p *progressLog) Checking(current string, force bool) {
	*p = append(*p, fmt.Sprintf("checking from %s, force %t", current, force))
}

func (p *progressLog) Updating(target string) { *p = append(*p, "updating to "+target) }

func TestUpdateReportsProgress(t *testing.T) {
	archive := tarGz(t, "new binary")
	sum := sha256.Sum256(archive)
	checksums := hex.EncodeToString(sum[:]) + "  " + ArchiveName() + "\n"
	type Given struct {
		running    string
		releases   []github.Release
		downloaded string
	}
	type When struct {
		force bool
	}
	type Then struct {
		progress progressLog
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"an update":  {Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v0.8.1")}, downloaded: "v0.8.1"}, Then: Then{progress: progressLog{"checking from v0.8.0, force false", "updating to v0.8.1"}}},
		"up to date": {Given: Given{running: "v0.8.1", releases: []github.Release{releaseOf("v0.8.1")}}, Then: Then{progress: progressLog{"checking from v0.8.1, force false"}}},
		"forced":     {Given: Given{running: "v0.8.0", releases: []github.Release{releaseOf("v1.0.0")}, downloaded: "v1.0.0"}, When: When{force: true}, Then: Then{progress: progressLog{"checking from v0.8.0, force true", "updating to v1.0.0"}}},
		"dev build":  {Given: Given{running: "dev"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			executable := filepath.Join(dir, "mse")
			require.NoError(t, os.WriteFile(executable, []byte("old binary"), 0o755))
			source := newMockReleaseSource(t)
			source.EXPECT().Releases(mock.Anything).Return(tt.Given.releases, nil).Maybe()
			source.EXPECT().Download(mock.Anything, int64(archiveID), mock.Anything).RunAndReturn(writing(archive)).Maybe()
			source.EXPECT().Download(mock.Anything, int64(checksumsID), mock.Anything).RunAndReturn(writing([]byte(checksums))).Maybe()
			versions := newMockVersionReader(t)
			versions.EXPECT().Version(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) (string, error) {
				return "mse " + tt.Given.downloaded + " (commit 1a2b3c4, built 2026-10-05)\n", nil
			}).Maybe()
			var progress progressLog

			_, _ = New(source, func() (string, error) { return executable, nil }, versions).
				Update(context.Background(), version.Build{Version: tt.Given.running}, tt.When.force, &progress)

			assert.Equal(t, tt.Then.progress, progress)
		})
	}
}
