package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/github"
	"github.com/pablovarela/media-server-engine/internal/release"
	"github.com/pablovarela/media-server-engine/internal/version"
)

type releaseSource interface {
	Releases(ctx context.Context) ([]github.Release, error)
	Download(ctx context.Context, asset github.Asset, w io.Writer) error
}

type versionReader interface {
	Version(ctx context.Context, path string) (string, error)
}

var ErrDevBuild = errors.New("dev builds don't update themselves; install a release with install.sh")

type Progress interface {
	Checking(current string, force bool)
	Updating(target string)
}

type Result struct {
	From       string
	To         string
	Path       string
	NewerMajor *github.Release
}

type Updater struct {
	source     releaseSource
	executable func() (string, error)
	versions   versionReader
}

func New(source releaseSource, executable func() (string, error), versions versionReader) *Updater {
	return &Updater{source: source, executable: executable, versions: versions}
}

func ArchiveName() string {
	return fmt.Sprintf("mse_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

func (u *Updater) Update(ctx context.Context, current version.Build, force bool, progress Progress) (Result, error) {
	if !semver.IsValid(current.Version) {
		return Result{}, ErrDevBuild
	}
	progress.Checking(current.Version, force)
	releases, err := u.source.Releases(ctx)
	if err != nil {
		return Result{}, err
	}
	target, newerMajor := release.Choose(current.Version, releases, force)
	result := Result{From: current.Version, NewerMajor: newerMajor}
	if target == nil {
		return result, nil
	}
	progress.Updating(target.Tag)
	path, err := u.install(ctx, *target)
	if err != nil {
		return Result{}, err
	}
	result.To, result.Path = target.Tag, path
	return result, nil
}

func (u *Updater) install(ctx context.Context, target github.Release) (string, error) {
	archive, err := u.download(ctx, target, ArchiveName())
	if err != nil {
		return "", err
	}
	checksums, err := u.download(ctx, target, "checksums.txt")
	if err != nil {
		return "", err
	}
	if err := release.VerifyChecksum(archive, checksums, ArchiveName()); err != nil {
		return "", fmt.Errorf("release %s: %w", target.Tag, err)
	}
	binary, err := release.ExtractBinary(archive)
	if err != nil {
		return "", fmt.Errorf("release %s: %w", target.Tag, err)
	}
	path, err := u.executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return path, u.replace(ctx, path, binary, target.Tag)
}

func (u *Updater) download(ctx context.Context, target github.Release, name string) ([]byte, error) {
	for _, asset := range target.Assets {
		if asset.Name == name {
			var content bytes.Buffer
			if err := u.source.Download(ctx, asset, &content); err != nil {
				return nil, err
			}
			return content.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("release %s has no %s", target.Tag, name)
}

func (u *Updater) replace(ctx context.Context, path string, binary []byte, tag string) error {
	dir := filepath.Dir(path)
	staged, err := os.CreateTemp(dir, ".mse-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(staged.Name()) }()
	if _, err := staged.Write(binary); err != nil {
		_ = staged.Close()
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	if err := os.Chmod(staged.Name(), 0o755); err != nil { //nolint:gosec // mse must stay executable
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	out, err := u.versions.Version(ctx, staged.Name())
	if err != nil {
		return fmt.Errorf("the downloaded mse %s does not run: %w", tag, err)
	}
	if fields := strings.Fields(out); len(fields) < 2 || fields[0] != "mse" || fields[1] != tag {
		return fmt.Errorf("the downloaded mse says %q, not %s", strings.TrimSpace(out), tag)
	}
	return os.Rename(staged.Name(), path)
}

type SystemVersionReader struct{}

func (SystemVersionReader) Version(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, path, "version").Output() //nolint:gosec // runs the binary this update just downloaded and checked
	return string(out), err
}
