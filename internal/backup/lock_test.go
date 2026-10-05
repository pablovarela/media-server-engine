package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".backup.lock")

	held, err := takeLock(path)
	require.NoError(t, err)
	_, err = takeLock(path)
	assert.EqualError(t, err, fmt.Sprintf("a backup is already running (process %d)", os.Getpid()))

	held.release()
	again, err := takeLock(path)
	require.NoError(t, err)
	again.release()
}
