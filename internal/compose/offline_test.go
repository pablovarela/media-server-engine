package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOffline(t *testing.T) {
	images := "services:\n  jellyfin:\n    image: jellyfin@sha256:j\n  homepage:\n    image: homepage@sha256:h\n  configarr:\n    image: configarr@sha256:c\n  portainer:\n    image: portainer@sha256:p\n"
	tests := map[string]struct {
		override string
		err      string
	}{
		"the stack loads":                     {},
		"with an override":                    {override: "services:\n  jellyfin:\n    environment:\n      EXTRA: yes\n"},
		"an override for a service not there": {override: "services:\n  nothing:\n    environment:\n      A: b\n", err: "nothing"},
		"an override that isn't YAML":         {override: "services: [\n", err: "compose.override.yml"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DOCKER_CONFIG", t.TempDir())
			config := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(config, "images.yml"), []byte(images), 0o644))
			if tt.override != "" {
				require.NoError(t, os.WriteFile(filepath.Join(config, "compose.override.yml"), []byte(tt.override), 0o644))
			}

			err := LoadOffline(context.Background(), os.DirFS("testdata/engine"), config, map[string]string{"TZ": "Europe/London"})

			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
