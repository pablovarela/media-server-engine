package create

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/secrets"
)

var template = fstest.MapFS{
	".gitignore":           {Data: []byte("secrets/*\n!secrets/*.sops.env\n")},
	"config.yml":           {Data: []byte("config: 0\n")},
	"configarr/config.yml": {Data: []byte("sonarr: {}\n")},
}

func TestWriteConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mse", "gorgon")

	undo, err := WriteConfig(template, dir, "gorgon", "age1recipient")

	require.NoError(t, err)
	for file, want := range map[string]string{
		".gitignore":           "secrets/*\n!secrets/*.sops.env\n",
		"config.yml":           "config: 0\n",
		"configarr/config.yml": "sonarr: {}\n",
		"installation.env":     "INSTALLATION_NAME=gorgon\n",
		".sops.yaml":           "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: age1recipient\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		require.NoError(t, err, file)
		assert.Equal(t, want, string(got), file)
	}
	require.NoError(t, undo())
	assert.NoDirExists(t, dir)
}

func TestWriteConfigRefusesAnExistingFolder(t *testing.T) {
	dir := t.TempDir()

	undo, err := WriteConfig(template, dir, "gorgon", "age1recipient")

	assert.Nil(t, undo)
	assert.ErrorContains(t, err, dir)
	assert.NoFileExists(t, filepath.Join(dir, "config.yml"))
}

func TestTheSopsRulesEncryptForTheNewKey(t *testing.T) {
	key, err := NewKey()
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), "gorgon")
	_, err = WriteConfig(template, dir, "gorgon", key.Public)
	require.NoError(t, err)
	path := filepath.Join(dir, "secrets", "apps.sops.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	encrypted, err := secrets.SopsEncrypter(t.TempDir(), filepath.Join(dir, ".sops.yaml"))(path, []byte("A=1\n"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encrypted, 0o644))
	for _, variable := range []string{"SOPS_AGE_KEY_FILE", "SOPS_AGE_KEY_CMD"} {
		t.Setenv(variable, "")
		require.NoError(t, os.Unsetenv(variable))
	}
	t.Setenv("SOPS_AGE_KEY", key.Secret)

	plain, err := secrets.Sops(t.TempDir())(path)

	require.NoError(t, err)
	assert.Equal(t, "A=1\n", string(plain))
}
