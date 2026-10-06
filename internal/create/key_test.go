package create

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var today = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func TestNewKeyIsAnAgeIdentity(t *testing.T) {
	key, err := NewKey()

	require.NoError(t, err)
	identity, err := age.ParseX25519Identity(key.Secret)
	require.NoError(t, err)
	assert.Equal(t, identity.Recipient().String(), key.Public)
}

func TestKeyFile(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(k string) string { return values[k] }
	}
	path, err := KeyFile(env(nil), "/home/p/.config")
	require.NoError(t, err)
	assert.Equal(t, "/home/p/.config/sops/age/keys.txt", path)

	path, err = KeyFile(env(map[string]string{"SOPS_AGE_KEY_FILE": "/keys/mine.txt"}), "/home/p/.config")
	require.NoError(t, err)
	assert.Equal(t, "/keys/mine.txt", path)

	for _, variable := range []string{"SOPS_AGE_KEY", "SOPS_AGE_KEY_CMD"} {
		_, err = KeyFile(env(map[string]string{variable: "x"}), "/home/p/.config")
		assert.EqualError(t, err, "the key goes into a key file, and "+variable+" is set: unset SOPS_AGE_KEY and SOPS_AGE_KEY_CMD (SOPS_AGE_KEY_FILE chooses the file)")
	}
}

func block(key Key) string {
	return "# media server gorgon, created 2026-10-06\n# public key: " + key.Public + "\n" + key.Secret + "\n"
}

func TestAppendKeyToAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sops", "age", "keys.txt")
	key := Key{Public: "age1pub", Secret: "AGE-SECRET-KEY-1X"}

	undo, err := AppendKey(path, "gorgon", "created", key, today)

	require.NoError(t, err)
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, block(key), string(written))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	folder, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), folder.Mode().Perm())

	require.NoError(t, undo())
	assert.NoFileExists(t, path)
}

func TestAppendKeyToAFileWithOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.txt")
	original := "# created: 2024-01-01\n# public key: age1old\nAGE-SECRET-KEY-1OLD\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
	key := Key{Public: "age1pub", Secret: "AGE-SECRET-KEY-1X"}

	undo, err := AppendKey(path, "gorgon", "created", key, today)

	require.NoError(t, err)
	written, _ := os.ReadFile(path)
	assert.Equal(t, original+block(key), string(written))
	require.NoError(t, undo())
	written, _ = os.ReadFile(path)
	assert.Equal(t, original, string(written))
}

func TestAppendKeyToAFileWithoutTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.txt")
	original := "AGE-SECRET-KEY-1OLD"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))
	key := Key{Public: "age1pub", Secret: "AGE-SECRET-KEY-1X"}

	undo, err := AppendKey(path, "gorgon", "created", key, today)

	require.NoError(t, err)
	written, _ := os.ReadFile(path)
	assert.Equal(t, original+"\n"+block(key), string(written))
	require.NoError(t, undo())
	written, _ = os.ReadFile(path)
	assert.Equal(t, original, string(written))
}

func TestUndoLeavesAKeyFileThatChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.txt")
	key := Key{Public: "age1pub", Secret: "AGE-SECRET-KEY-1X"}
	undo, err := AppendKey(path, "gorgon", "created", key, today)
	require.NoError(t, err)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, _ = f.WriteString("AGE-SECRET-KEY-1OTHER\n")
	require.NoError(t, f.Close())

	err = undo()

	var changed *KeyFileChangedError
	require.ErrorAs(t, err, &changed)
	assert.Equal(t, path, changed.Path)
	assert.EqualError(t, err, "the new key stays in "+path+", which changed while mse create ran; remove its lines (# media server gorgon) by hand")
	written, _ := os.ReadFile(path)
	assert.True(t, bytes.HasSuffix(written, []byte("AGE-SECRET-KEY-1OTHER\n")))
	assert.True(t, strings.HasPrefix(string(written), block(key)))
}

func TestAppendKeyNamesHowTheKeyCame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.txt")

	_, err := AppendKey(path, "gorgon", "added", Key{Public: "age1pub", Secret: "AGE-SECRET-KEY-1X"}, today)

	require.NoError(t, err)
	written, _ := os.ReadFile(path)
	assert.True(t, strings.HasPrefix(string(written), "# media server gorgon, added 2026-10-06\n"))
}
