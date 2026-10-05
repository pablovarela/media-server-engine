package logfile

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gunzipped(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	text, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(text)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var found []string
	for _, entry := range entries {
		if entry.Name() != ".rotate.lock" {
			found = append(found, entry.Name())
		}
	}
	return found
}

func TestRotation(t *testing.T) {
	type Given struct {
		previous   string
		compressed int
	}
	type Then struct {
		files []string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the first rotation keeps the file plain": {
			Then: Then{files: []string{"mse.log.1"}},
		},
		"the previous rotation is compressed": {
			Given: Given{previous: "the run before\n"},
			Then:  Then{files: []string{"mse.log.1", "mse.log.2.gz"}},
		},
		"five compressed are kept": {
			Given: Given{previous: "the run before\n", compressed: 5},
			Then:  Then{files: []string{"mse.log.1", "mse.log.2.gz", "mse.log.3.gz", "mse.log.4.gz", "mse.log.5.gz", "mse.log.6.gz"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log"), []byte("a long enough log\n"), 0o644))
			if tt.Given.previous != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log.1"), []byte(tt.Given.previous), 0o644))
			}
			for n := 2; n < 2+tt.Given.compressed; n++ {
				require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("mse.log.%d.gz", n)), []byte("old"), 0o644))
			}

			require.NoError(t, rotate(dir, 10, 5))

			assert.Equal(t, tt.Then.files, names(t, dir))
			plain, err := os.ReadFile(filepath.Join(dir, "mse.log.1"))
			require.NoError(t, err)
			assert.Equal(t, "a long enough log\n", string(plain))
			if tt.Given.previous != "" {
				assert.Equal(t, tt.Given.previous, gunzipped(t, filepath.Join(dir, "mse.log.2.gz")))
			}
		})
	}
}

func TestUnderTheLimitNothingRotates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log"), []byte("small\n"), 0o644))

	require.NoError(t, rotate(dir, 10, 5))

	assert.Equal(t, []string{"mse.log"}, names(t, dir))
}

func TestARunWritingDuringARotationKeepsItsLines(t *testing.T) {
	dir := t.TempDir()
	running := New(Options{Limit: 10, Keep: 5, RunID: "aaaaaa", Now: noon, Warn: &bytes.Buffer{}})
	running.Open(dir)
	running.Line("", "Stopping the stack... a line long enough to pass the limit")
	other := New(Options{Limit: 10, Keep: 5, RunID: "bbbbbb", Now: noon, Warn: &bytes.Buffer{}})
	other.Open(dir)

	running.Line("", "Backup done.")
	running.Close()
	other.Close()

	plain, err := os.ReadFile(filepath.Join(dir, "mse.log.1"))
	require.NoError(t, err)
	assert.Contains(t, string(plain), "Backup done.")
}

func TestRotationSkipsWhileLocked(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log"), []byte("a long enough log\n"), 0o644))
	held, err := os.OpenFile(filepath.Join(dir, ".rotate.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))

	require.NoError(t, rotate(dir, 10, 5))

	assert.Equal(t, []string{"mse.log"}, names(t, dir))
}

func TestAFailedRotationStillLogs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log"), []byte("a long enough log\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "mse.log.1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log.1", "blocker"), nil, 0o644))
	var warn bytes.Buffer
	f := New(Options{Limit: 10, Keep: 5, RunID: "a1b2c3", Now: noon, Warn: &warn})

	f.Open(dir)
	f.Line("", "still logged")
	f.Close()

	logged, err := os.ReadFile(filepath.Join(dir, "mse.log"))
	require.NoError(t, err)
	assert.Contains(t, string(logged), "still logged")
	assert.Contains(t, warn.String(), "could not rotate the log")
	assert.NoFileExists(t, filepath.Join(dir, "mse.log.2.gz"), "a partial compressed file is removed")
}
