package wiring

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhatTheWiringRemembersIsReadableOnlyByTheOwner(t *testing.T) {
	state := State{Dir: filepath.Join(t.TempDir(), "volumes", ".wiring")}
	assert.Empty(t, state.Remembered("app.key"))

	require.NoError(t, state.Remember("app.key", "new-key"))

	assert.Equal(t, "new-key", state.Remembered("app.key"))
	info, err := os.Stat(filepath.Join(state.Dir, "app.key"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	content, err := os.ReadFile(filepath.Join(state.Dir, "app.key"))
	require.NoError(t, err)
	assert.Equal(t, "new-key\n", string(content))
}

func TestFingerprintIsTheSHA256OfTheValuesJoinedByNUL(t *testing.T) {
	assert.Equal(t, "f861d6ff4ebbb25eaeff6d5d4ec9eb758352f4bd4167aacb6a8ebd035f4a255b", Fingerprint("Sonarr", "key"))
}
