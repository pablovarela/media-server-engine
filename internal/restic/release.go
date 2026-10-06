package restic

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

//go:embed release.env
var releaseFile string

const checksumPrefix = "RESTIC_SHA256_"

type Release struct {
	Version string
	SHA256  map[string]string
}

func Pinned() (Release, error) {
	release := Release{SHA256: map[string]string{}}
	for _, line := range strings.Split(releaseFile, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		switch {
		case !ok:
		case key == "RESTIC_VERSION":
			release.Version = value
		case strings.HasPrefix(key, checksumPrefix):
			release.SHA256[strings.TrimPrefix(key, checksumPrefix)] = value
		}
	}
	if release.Version == "" {
		return Release{}, errors.New("release.env has no RESTIC_VERSION")
	}
	return release, nil
}

func (r Release) Archive(goos, goarch string) (name, sha256 string, err error) {
	sha256 = r.SHA256[goos+"_"+goarch]
	if sha256 == "" {
		return "", "", fmt.Errorf("this mse has no pinned restic for %s/%s", goos, goarch)
	}
	return fmt.Sprintf("restic_%s_%s_%s.bz2", r.Version, goos, goarch), sha256, nil
}
