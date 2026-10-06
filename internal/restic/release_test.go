package restic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinnedHasEveryReleasedPlatform(t *testing.T) {
	release, err := Pinned()

	require.NoError(t, err)
	assert.Regexp(t, `^\d+\.\d+\.\d+$`, release.Version)
	for _, platform := range []string{"linux_arm64", "linux_amd64", "darwin_arm64", "darwin_amd64"} {
		assert.Regexp(t, `^[0-9a-f]{64}$`, release.SHA256[platform], platform)
	}
}

func TestArchive(t *testing.T) {
	release := Release{Version: "0.19.1", SHA256: map[string]string{"linux_arm64": "abc"}}

	name, sum, err := release.Archive("linux", "arm64")
	require.NoError(t, err)
	assert.Equal(t, "restic_0.19.1_linux_arm64.bz2", name)
	assert.Equal(t, "abc", sum)

	_, _, err = release.Archive("linux", "386")
	assert.EqualError(t, err, "this mse has no pinned restic for linux/386")
}
