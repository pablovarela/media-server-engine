package restic

import (
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pablovarela/media-server-engine/internal/report"
)

const releases = "https://github.com/restic/restic/releases/download/"

const (
	defaultMaxArchive = 64 << 20
	staleDownload     = time.Hour
)

type Fetch struct {
	Release      Release
	Cache        string
	GOOS, GOARCH string
	HTTP         *http.Client
	Report       *report.Reporter
	MaxArchive   int64
}

func Binary(ctx context.Context, f Fetch) (string, error) {
	folder := filepath.Join(f.Cache, f.Release.Version)
	path := filepath.Join(folder, "restic")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	name, sum, err := f.Release.Archive(f.GOOS, f.GOARCH)
	if err != nil {
		return "", err
	}
	step := f.Report.Step("Downloading restic " + f.Release.Version)
	if err := f.install(ctx, folder, path, name, sum); err != nil {
		_ = os.Remove(folder)
		return "", step.FailWithoutTail(err)
	}
	removeStaleDownloads(folder)
	f.removeOlderVersions()
	step.Done("done")
	return path, nil
}

func (f Fetch) install(ctx context.Context, folder, path, name, sum string) error {
	archive, err := f.download(ctx, name)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != sum {
		return fmt.Errorf("%s doesn't match its pinned checksum; nothing was installed", name)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil { //nolint:gosec // a cache folder of downloaded tools
		return err
	}
	temporary, err := os.CreateTemp(folder, "restic-*.part")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	_, copied := io.Copy(temporary, bzip2.NewReader(bytes.NewReader(archive)))
	closed := temporary.Close()
	if copied != nil {
		return fmt.Errorf("%s isn't a bzip2 archive (%w)", name, copied)
	}
	if closed != nil {
		return closed
	}
	if err := os.Chmod(temporary.Name(), 0o755); err != nil { //nolint:gosec // an executable
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func (f Fetch) download(ctx context.Context, name string) ([]byte, error) {
	failed := func(reason error) error {
		return fmt.Errorf("could not download restic %s from github.com/restic/restic (%w)", f.Release.Version, reason)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releases+"v"+f.Release.Version+"/"+name, nil)
	if err != nil {
		return nil, failed(err)
	}
	response, err := f.HTTP.Do(request)
	if err != nil {
		return nil, failed(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, failed(fmt.Errorf("GitHub answered %d %s", response.StatusCode, http.StatusText(response.StatusCode)))
	}
	limit := f.MaxArchive
	if limit == 0 {
		limit = defaultMaxArchive
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	switch {
	case err != nil:
		return nil, failed(err)
	case int64(len(body)) > limit:
		return nil, failed(fmt.Errorf("the archive is larger than %d bytes", limit))
	}
	return body, nil
}

func (f Fetch) removeOlderVersions() {
	entries, err := os.ReadDir(f.Cache)
	if err != nil {
		return
	}
	var others []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != f.Release.Version {
			others = append(others, entry)
		}
	}
	sort.Slice(others, func(a, b int) bool { return modified(others[a]).After(modified(others[b])) })
	for _, older := range others[min(1, len(others)):] {
		_ = os.RemoveAll(filepath.Join(f.Cache, older.Name()))
	}
}

func modified(entry os.DirEntry) time.Time {
	info, err := entry.Info()
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func removeStaleDownloads(folder string) {
	parts, _ := filepath.Glob(filepath.Join(folder, "restic-*.part"))
	for _, part := range parts {
		if info, err := os.Stat(part); err == nil && time.Since(info.ModTime()) > staleDownload {
			_ = os.Remove(part)
		}
	}
}
