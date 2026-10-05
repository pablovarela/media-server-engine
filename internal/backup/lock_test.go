package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestWaitWhileRunning(t *testing.T) {
	t.Run("no backup running", func(t *testing.T) {
		announced := 0
		err := WaitWhileRunning(filepath.Join(t.TempDir(), ".backup.lock"), Waiting{
			Timeout: time.Hour, Poll: 10 * time.Second, Now: time.Now,
			Sleep:    func(time.Duration) { t.Fatal("no wait needed") },
			Announce: func() { announced++ },
		})

		require.NoError(t, err)
		assert.Zero(t, announced)
	})

	t.Run("waits until the backup finishes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".backup.lock")
		held, err := takeLock(path)
		require.NoError(t, err)
		announced, slept := 0, 0

		err = WaitWhileRunning(path, Waiting{
			Timeout: time.Hour, Poll: 10 * time.Second, Now: time.Now,
			Sleep: func(d time.Duration) {
				assert.Equal(t, 10*time.Second, d)
				slept++
				if slept == 2 {
					held.release()
				}
			},
			Announce: func() { announced++ },
		})

		require.NoError(t, err)
		assert.Equal(t, 1, announced)
		assert.Equal(t, 2, slept)
	})

	t.Run("gives up after the timeout", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".backup.lock")
		held, err := takeLock(path)
		require.NoError(t, err)
		defer held.release()
		now := time.Date(2026, 10, 6, 5, 0, 0, 0, time.UTC)

		err = WaitWhileRunning(path, Waiting{
			Timeout: time.Hour, Poll: 10 * time.Minute, Now: func() time.Time { return now },
			Sleep: func(d time.Duration) { now = now.Add(d) }, Announce: func() {},
		})

		assert.ErrorIs(t, err, ErrStillRunning)
	})
}
