package backup

import (
	"context"
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
		err := WaitWhileRunning(context.Background(), filepath.Join(t.TempDir(), ".backup.lock"), Waiting{
			Timeout: time.Hour, Poll: 10 * time.Second, Now: time.Now,
			Sleep:    func(context.Context, time.Duration) error { t.Fatal("no wait needed"); return nil },
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

		err = WaitWhileRunning(context.Background(), path, Waiting{
			Timeout: time.Hour, Poll: 10 * time.Second, Now: time.Now,
			Sleep: func(_ context.Context, d time.Duration) error {
				assert.Equal(t, 10*time.Second, d)
				slept++
				if slept == 2 {
					held.release()
				}
				return nil
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

		err = WaitWhileRunning(context.Background(), path, Waiting{
			Timeout: time.Hour, Poll: 10 * time.Minute, Now: func() time.Time { return now },
			Sleep: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }, Announce: func() {},
		})

		assert.ErrorIs(t, err, ErrStillRunning)
	})

	t.Run("stops waiting when interrupted", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".backup.lock")
		held, err := takeLock(path)
		require.NoError(t, err)
		defer held.release()

		err = WaitWhileRunning(context.Background(), path, Waiting{
			Timeout: time.Hour, Poll: 10 * time.Second, Now: time.Now, Announce: func() {},
			Sleep: func(context.Context, time.Duration) error { return context.Canceled },
		})

		assert.ErrorIs(t, err, context.Canceled)
	})
}
