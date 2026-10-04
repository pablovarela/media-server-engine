package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/pablovarela/media-server-engine/internal/github"
)

const maxBinarySize = 256 << 20

func Choose(current string, releases []github.Release, force bool) (target, newerMajor *github.Release) {
	for i := range releases {
		candidate := &releases[i]
		if !semver.IsValid(candidate.Tag) || semver.Compare(candidate.Tag, current) <= 0 {
			continue
		}
		sameMajor := semver.Major(candidate.Tag) == semver.Major(current)
		if (sameMajor || force) && newer(candidate, target) {
			target = candidate
		}
		if !sameMajor && newer(candidate, newerMajor) {
			newerMajor = candidate
		}
	}
	if force {
		newerMajor = nil
	}
	return target, newerMajor
}

func newer(candidate, than *github.Release) bool {
	return than == nil || semver.Compare(candidate.Tag, than.Tag) > 0
}

func VerifyChecksum(archive, checksums []byte, name string) error {
	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			if fields[0] != actual {
				return fmt.Errorf("checksum mismatch for %s", name)
			}
			return nil
		}
	}
	return fmt.Errorf("checksums.txt has no line for %s", name)
}

func ExtractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read the archive: %w", err)
	}
	files := tar.NewReader(gz)
	for {
		header, err := files.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive has no mse")
		}
		if err != nil {
			return nil, fmt.Errorf("read the archive: %w", err)
		}
		if header.Name == "mse" && header.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(files, maxBinarySize))
		}
	}
}
