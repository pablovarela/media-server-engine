package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/github"
)

func TestChoose(t *testing.T) {
	type Given struct {
		current  string
		releases []string
		force    bool
	}
	type Then struct {
		target     string
		newerMajor string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"newer patch": {
			Given: Given{current: "v0.8.0", releases: []string{"v0.8.0", "v0.8.1"}},
			Then:  Then{target: "v0.8.1"},
		},
		"every 0.x release is the same major": {
			Given: Given{current: "v0.8.0", releases: []string{"v0.8.1", "v0.9.0", "v0.10.0"}},
			Then:  Then{target: "v0.10.0"},
		},
		"newer minor from 1.0 on": {
			Given: Given{current: "v1.2.0", releases: []string{"v1.3.0", "v1.2.5"}},
			Then:  Then{target: "v1.3.0"},
		},
		"newer major is reported, not chosen": {
			Given: Given{current: "v1.2.0", releases: []string{"v1.2.1", "v2.0.0", "v2.1.0"}},
			Then:  Then{target: "v1.2.1", newerMajor: "v2.1.0"},
		},
		"only a newer major": {
			Given: Given{current: "v0.9.0", releases: []string{"v0.9.0", "v1.0.0"}},
			Then:  Then{newerMajor: "v1.0.0"},
		},
		"force chooses the newest release across majors": {
			Given: Given{current: "v1.2.0", releases: []string{"v1.2.1", "v2.1.0", "v2.0.0"}, force: true},
			Then:  Then{target: "v2.1.0"},
		},
		"nothing newer": {
			Given: Given{current: "v0.8.0", releases: []string{"v0.7.0", "v0.8.0"}},
			Then:  Then{},
		},
		"running newer than every release": {
			Given: Given{current: "v0.9.0", releases: []string{"v0.8.0"}},
			Then:  Then{},
		},
		"tags that are not versions are ignored": {
			Given: Given{current: "v0.8.0", releases: []string{"nightly", "v0.8.1", "latest"}},
			Then:  Then{target: "v0.8.1"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var releases []github.Release
			for _, tag := range tt.Given.releases {
				releases = append(releases, github.Release{Tag: tag})
			}

			target, newerMajor := Choose(tt.Given.current, releases, tt.Given.force)

			assert.Equal(t, tt.Then.target, tagOf(target))
			assert.Equal(t, tt.Then.newerMajor, tagOf(newerMajor))
		})
	}
}

func tagOf(r *github.Release) string {
	if r == nil {
		return ""
	}
	return r.Tag
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("archive bytes")
	sum := sha256.Sum256(archive)
	good := hex.EncodeToString(sum[:])

	type Given struct {
		checksums string
	}
	type Then struct {
		err string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"matching line": {
			Given: Given{checksums: "0000  mse_darwin_arm64.tar.gz\n" + good + "  mse_linux_arm64.tar.gz\n"},
		},
		"mismatch": {
			Given: Given{checksums: "0000  mse_linux_arm64.tar.gz\n"},
			Then:  Then{err: "checksum mismatch for mse_linux_arm64.tar.gz"},
		},
		"no line for the archive": {
			Given: Given{checksums: good + "  mse_darwin_arm64.tar.gz\n"},
			Then:  Then{err: "checksums.txt has no line for mse_linux_arm64.tar.gz"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := VerifyChecksum(archive, []byte(tt.Given.checksums), "mse_linux_arm64.tar.gz")

			if tt.Then.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.Then.err)
			}
		})
	}
}

func TestExtractBinary(t *testing.T) {
	type Given struct {
		archive []byte
	}
	type Then struct {
		binary string
		err    string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"archive holding mse": {
			Given: Given{archive: tarGz(t, map[string]string{"mse": "new binary"})},
			Then:  Then{binary: "new binary"},
		},
		"archive without mse": {
			Given: Given{archive: tarGz(t, map[string]string{"README.md": "hello"})},
			Then:  Then{err: "the archive has no mse"},
		},
		"not a gzip archive": {
			Given: Given{archive: []byte("this is a text file, not a gzip archive")},
			Then:  Then{err: "read the archive: gzip: invalid header"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			binary, err := ExtractBinary(tt.Given.archive)

			if tt.Then.err != "" {
				assert.EqualError(t, err, tt.Then.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.Then.binary, string(binary))
		})
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}
