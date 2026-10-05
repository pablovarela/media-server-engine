package homepage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncImages(t *testing.T) {
	i := fixture(t, "main")
	images := filepath.Join(i.Config, "homepage", "images")
	require.NoError(t, os.MkdirAll(images, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(images, "background.svg"), []byte("<svg/>"), 0o644))

	changed, err := syncImages(i)
	require.NoError(t, err)
	assert.True(t, changed, "first copy")

	changed, err = syncImages(i)
	require.NoError(t, err)
	assert.False(t, changed, "nothing new")

	require.NoError(t, os.Remove(filepath.Join(images, "background.svg")))
	changed, err = syncImages(i)
	require.NoError(t, err)
	assert.True(t, changed, "a removal is a change")
	assert.NoFileExists(t, filepath.Join(i.State, ".homepage-images", "background.svg"))
}

func TestSyncImagesWithoutAnImagesFolder(t *testing.T) {
	changed, err := syncImages(fixture(t, "main"))

	require.NoError(t, err)
	assert.False(t, changed)
}
