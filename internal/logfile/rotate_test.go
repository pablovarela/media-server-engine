package logfile

import (
	"compress/gzip"
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

func TestRotation(t *testing.T) {
	type Given struct {
		current string
		older   int
	}
	type Then struct {
		rotated bool
		files   []string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"under the limit": {Given: Given{current: "small\n"}, Then: Then{files: []string{"mse.log"}}},
		"over the limit":  {Given: Given{current: "a long enough log\n"}, Then: Then{rotated: true, files: []string{"mse.log.1.gz"}}},
		"keeps five": {
			Given: Given{current: "a long enough log\n", older: 5},
			Then:  Then{rotated: true, files: []string{"mse.log.1.gz", "mse.log.2.gz", "mse.log.3.gz", "mse.log.4.gz", "mse.log.5.gz"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log"), []byte(tt.Given.current), 0o644))
			for n := 1; n <= tt.Given.older; n++ {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log."+string(rune('0'+n))+".gz"), []byte("old"), 0o644))
			}

			require.NoError(t, rotate(dir, 10, 5))

			var found []string
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			for _, entry := range entries {
				if entry.Name() != ".rotate.lock" {
					found = append(found, entry.Name())
				}
			}
			assert.Equal(t, tt.Then.files, found)
			if tt.Then.rotated {
				assert.Equal(t, tt.Given.current, gunzipped(t, filepath.Join(dir, "mse.log.1.gz")))
			}
		})
	}
}

func TestRotationSkipsWhileLocked(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mse.log"), []byte("a long enough log\n"), 0o644))
	held, err := os.OpenFile(filepath.Join(dir, ".rotate.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))

	require.NoError(t, rotate(dir, 10, 5))

	assert.FileExists(t, filepath.Join(dir, "mse.log"))
	assert.NoFileExists(t, filepath.Join(dir, "mse.log.1.gz"))
}
