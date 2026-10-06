package restic

import (
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pablovarela/media-server-engine/internal/report"
)

const (
	releases      = "https://github.com/restic/restic/releases/download/"
	staleDownload = time.Hour
	keepRecent    = 30 * 24 * time.Hour
	checksumFile  = ".sha256"
)

var maxArchive int64 = 64 << 20

type Fetch struct {
	Release      Release
	Cache        string
	GOOS, GOARCH string
	HTTP         *http.Client
	Report       *report.Reporter
}

func Binary(ctx context.Context, f Fetch) (string, error) {
	folder := filepath.Join(f.Cache, f.Release.Version)
	path := filepath.Join(folder, "restic")
	if intact(path) {
		return path, nil
	}
	name, sum, err := f.Release.Archive(f.GOOS, f.GOARCH)
	if err != nil {
		return "", err
	}
	step := f.Report.Step("Downloading restic " + f.Release.Version)
	if err := f.install(ctx, folder, path, name, sum); err != nil {
		return "", step.FailWithoutTail(err)
	}
	removeStaleDownloads(folder)
	f.removeOlderVersions()
	step.Done("done")
	return path, nil
}

func intact(path string) bool {
	recorded, err := os.ReadFile(path + checksumFile) //nolint:gosec // the checksum mse wrote next to its restic
	if err != nil {
		return false
	}
	got, err := fileSum(path)
	return err == nil && got == strings.TrimSpace(string(recorded))
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // the cached restic
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (f Fetch) install(ctx context.Context, folder, path, name, sum string) error {
	if err := os.MkdirAll(folder, 0o755); err != nil { //nolint:gosec // a cache folder of downloaded tools
		return err
	}
	archive, err := os.CreateTemp(folder, "restic-*.bz2.part")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archive.Name()) }()
	got, err := f.download(ctx, name, archive)
	if closed := archive.Close(); err == nil {
		err = closed
	}
	if err != nil {
		return err
	}
	if got != sum {
		return fmt.Errorf("%s doesn't match its pinned checksum; nothing was installed", name)
	}
	binary, err := decompress(archive.Name(), folder, name)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(binary) }()
	binarySum, err := fileSum(binary)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+checksumFile, []byte(binarySum+"\n"), 0o644); err != nil { //nolint:gosec // a checksum, not a secret
		return err
	}
	return os.Rename(binary, path)
}

func decompress(archive, folder, name string) (string, error) {
	in, err := os.Open(archive) //nolint:gosec // the archive this call downloaded
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.CreateTemp(folder, "restic-*.part")
	if err != nil {
		return "", err
	}
	_, copied := io.Copy(out, bzip2.NewReader(in))
	closed := out.Close()
	switch {
	case copied != nil:
		_ = os.Remove(out.Name())
		return "", fmt.Errorf("%s isn't a bzip2 archive (%w)", name, copied)
	case closed != nil:
		_ = os.Remove(out.Name())
		return "", closed
	}
	if err := os.Chmod(out.Name(), 0o755); err != nil { //nolint:gosec // an executable
		_ = os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

func (f Fetch) download(ctx context.Context, name string, to io.Writer) (string, error) {
	failed := func(reason error) error {
		return fmt.Errorf("could not download restic %s from github.com/restic/restic (%w)", f.Release.Version, reason)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releases+"v"+f.Release.Version+"/"+name, nil)
	if err != nil {
		return "", failed(err)
	}
	response, err := f.HTTP.Do(request)
	if err != nil {
		return "", failed(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", failed(fmt.Errorf("GitHub answered %d %s", response.StatusCode, http.StatusText(response.StatusCode)))
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(to, hash), io.LimitReader(response.Body, maxArchive+1))
	switch {
	case err != nil:
		return "", failed(err)
	case written > maxArchive:
		return "", failed(fmt.Errorf("the archive is larger than %d bytes", maxArchive))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (f Fetch) removeOlderVersions() {
	others := f.otherVersions()
	newest := ""
	for _, entry := range others {
		if newer(entry.Name(), newest) {
			newest = entry.Name()
		}
	}
	for _, entry := range others {
		if entry.Name() != newest && !recentlyTouched(entry) {
			_ = os.RemoveAll(filepath.Join(f.Cache, entry.Name()))
		}
	}
}

func (f Fetch) otherVersions() []os.DirEntry {
	entries, _ := os.ReadDir(f.Cache)
	var others []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != f.Release.Version {
			others = append(others, entry)
		}
	}
	return others
}

func recentlyTouched(entry os.DirEntry) bool {
	info, err := entry.Info()
	return err != nil || time.Since(info.ModTime()) < keepRecent
}

func newer(version, than string) bool {
	if than == "" {
		return true
	}
	a, b := strings.Split(version, "."), strings.Split(than, ".")
	for n := 0; n < len(a) && n < len(b); n++ {
		x, errX := strconv.Atoi(a[n])
		y, errY := strconv.Atoi(b[n])
		if errX != nil || errY != nil {
			return version > than
		}
		if x != y {
			return x > y
		}
	}
	return len(a) > len(b)
}

func removeStaleDownloads(folder string) {
	parts, _ := filepath.Glob(filepath.Join(folder, "restic-*.part"))
	for _, part := range parts {
		if info, err := os.Stat(part); err == nil && time.Since(info.ModTime()) > staleDownload {
			_ = os.Remove(part)
		}
	}
}
