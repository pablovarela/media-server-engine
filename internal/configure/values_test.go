package configure

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadReadsTheFilesThatExist(t *testing.T) {
	config := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(config, PlainFile), []byte(gorgonEnv), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(config, "secrets", "apps.sops.env"), []byte("encrypted"), 0o644))
	decrypt := func(path string) ([]byte, error) {
		assert.Equal(t, filepath.Join(config, "secrets", "apps.sops.env"), path)
		return []byte("SONARR_API_KEY=sonarr-key\nJELLYFIN_ADMIN_PASSWORD=pa=ss\n"), nil
	}

	values, texts, err := Load(config, decrypt)

	require.NoError(t, err)
	assert.Equal(t, "~/backups", values.Get(PlainFile, "RESTIC_REPOSITORY"))
	assert.Equal(t, "pa=ss", values.Get("secrets/apps.sops.env", "JELLYFIN_ADMIN_PASSWORD"))
	assert.Empty(t, values.Get("secrets/vpn.sops.env", "OPENVPN_PASSWORD"))
	assert.Equal(t, gorgonEnv, string(texts[PlainFile]))
	assert.Equal(t, "SONARR_API_KEY=sonarr-key\nJELLYFIN_ADMIN_PASSWORD=pa=ss\n", string(texts["secrets/apps.sops.env"]))
	assert.Empty(t, texts["secrets/vpn.sops.env"])
}

func TestLoadNamesTheFileItCannotDecrypt(t *testing.T) {
	config := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(config, "secrets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(config, "secrets", "vpn.sops.env"), []byte("garbage"), 0o644))

	_, _, err := Load(config, func(string) ([]byte, error) { return nil, errors.New("Error unmarshalling input json") })

	require.EqualError(t, err, "could not read secrets/vpn.sops.env: Error unmarshalling input json")
}

func TestWithLeavesTheOriginalAlone(t *testing.T) {
	before := Values{PlainFile: {"TZ": "Europe/London"}}

	after := before.With(PlainFile, "TZ", "Europe/Madrid")

	assert.Equal(t, "Europe/London", before.Get(PlainFile, "TZ"))
	assert.Equal(t, "Europe/Madrid", after.Get(PlainFile, "TZ"))
}
