package logfile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var noon = func() time.Time { return time.Date(2026, 10, 5, 13, 31, 2, 0, time.Local) }

func read(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(text)
}

func TestLinesAreBufferedUntilTheFileOpens(t *testing.T) {
	dir := t.TempDir()
	f := New(Options{Limit: 1 << 20, Keep: 5, RunID: "a1b2c3", Now: noon, Warn: &bytes.Buffer{}})
	f.SetCommand("backup")
	f.Line("", "start backup")
	f.Line("restic", "snapshot 40c4a929 saved \x1b[32min colour\x1b[0m")

	f.Open(dir)
	f.Line("", "finish exit 0 after 5s")
	f.Close()

	assert.Equal(t, "2026-10-05 13:31:02 backup[a1b2c3] start backup\n"+
		"2026-10-05 13:31:02 backup[a1b2c3] restic | snapshot 40c4a929 saved in colour\n"+
		"2026-10-05 13:31:02 backup[a1b2c3] finish exit 0 after 5s\n", read(t, filepath.Join(dir, "mse.log")))
}

func TestLinesReachTheFileAsTheyHappen(t *testing.T) {
	dir := t.TempDir()
	f := New(Options{Limit: 1 << 20, Keep: 5, RunID: "a1b2c3", Now: noon, Warn: &bytes.Buffer{}})
	f.SetCommand("backup")
	f.Open(dir)

	f.Line("", "Stopping the stack...")

	assert.Contains(t, read(t, filepath.Join(dir, "mse.log")), "Stopping the stack...")
	f.Close()
}

func TestLinesFromTwoWritersStayWhole(t *testing.T) {
	dir := t.TempDir()
	first := New(Options{Limit: 1 << 20, Keep: 5, RunID: "aaaaaa", Now: noon, Warn: &bytes.Buffer{}})
	second := New(Options{Limit: 1 << 20, Keep: 5, RunID: "bbbbbb", Now: noon, Warn: &bytes.Buffer{}})
	first.Open(dir)
	second.Open(dir)
	var wg sync.WaitGroup
	for _, f := range []*File{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				f.Line("restic", strings.Repeat("x", 200))
			}
		}()
	}
	wg.Wait()
	first.Close()
	second.Close()

	lines := strings.Split(strings.TrimSuffix(read(t, filepath.Join(dir, "mse.log")), "\n"), "\n")
	require.Len(t, lines, 1000)
	for _, line := range lines {
		assert.True(t, strings.HasSuffix(line, "restic | "+strings.Repeat("x", 200)), line)
	}
}

func TestAnUnusableLogWarnsOnce(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocked, nil, 0o644))
	var warn bytes.Buffer
	f := New(Options{Limit: 1 << 20, Keep: 5, RunID: "a1b2c3", Now: noon, Warn: &warn})

	f.Open(filepath.Join(blocked, "logs"))
	f.Line("", "carries on")
	f.Close()

	assert.Equal(t, 1, strings.Count(warn.String(), "could not write the log "))
	assert.Contains(t, warn.String(), "; carrying on without it\n")
	assert.False(t, f.Opened())
}

func TestAWriteFailureIsReturnedOnce(t *testing.T) {
	f := New(Options{Limit: 1 << 20, Keep: 5, RunID: "a1b2c3", Now: noon, Warn: &bytes.Buffer{}})
	f.Open(t.TempDir())
	_ = f.file.Close()

	first := f.Line("", "lost")
	second := f.Line("", "lost too")

	assert.Contains(t, first, "could not write the log ")
	assert.Contains(t, first, "; carrying on without it")
	assert.Empty(t, second)
}

func TestAFailureWhileFlushingStopsTheFlush(t *testing.T) {
	var warn bytes.Buffer
	f := New(Options{Limit: 1 << 20, Keep: 5, RunID: "a1b2c3", Now: noon, Warn: &warn})
	f.Line("", "start backup")
	f.Line("", "Stopping the stack...")
	closed, err := os.Create(filepath.Join(t.TempDir(), "mse.log"))
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	f.file = closed

	assert.NotPanics(t, f.flush)

	assert.Equal(t, 1, strings.Count(warn.String(), "could not write the log "))
}
