package backup

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineID(t *testing.T) {
	t.Run("the system's", func(t *testing.T) {
		system := filepath.Join(t.TempDir(), "machine-id")
		require.NoError(t, os.WriteFile(system, []byte("abc123\n"), 0o644))

		id, err := MachineID(system, t.TempDir())

		require.NoError(t, err)
		assert.Equal(t, "abc123", id)
	})
	t.Run("generated once when the system has none", func(t *testing.T) {
		data := t.TempDir()
		missing := filepath.Join(t.TempDir(), "machine-id")

		first, err := MachineID(missing, data)
		require.NoError(t, err)
		again, err := MachineID(missing, data)
		require.NoError(t, err)

		assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), first)
		assert.Equal(t, first, again)
	})
}
