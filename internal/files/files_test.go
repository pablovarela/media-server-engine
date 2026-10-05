package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteIfChanged(t *testing.T) {
	type Given struct {
		existing *string
	}
	type Then struct {
		changed bool
	}
	text := func(s string) *string { return &s }
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"new file":      {Then: Then{changed: true}},
		"same content":  {Given: Given{existing: text("content")}, Then: Then{changed: false}},
		"other content": {Given: Given{existing: text("old")}, Then: Then{changed: true}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sub", "file")
			past := time.Now().Add(-time.Hour)
			if tt.Given.existing != nil {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(*tt.Given.existing), 0o600))
				require.NoError(t, os.Chtimes(path, past, past))
			}

			changed, err := WriteIfChanged(path, []byte("content"), 0o600)

			require.NoError(t, err)
			assert.Equal(t, tt.Then.changed, changed)
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "content", string(got))
			if !tt.Then.changed {
				info, err := os.Stat(path)
				require.NoError(t, err)
				assert.WithinDuration(t, past, info.ModTime(), time.Second)
			}
		})
	}
}
