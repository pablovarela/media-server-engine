package homepage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

var defaultPage = fstest.MapFS{
	"homepage/settings.yaml":  {Data: []byte("title: \"@INSTALLATION_NAME@\"\n")},
	"homepage/services.yaml":  {Data: []byte("- Healthchecks:\n    - Update:\n        description: \"@HEALTHCHECK_UPDATE@\"\n- Apps:\n    - Home:\n        href: http://@HOST@\n")},
	"homepage/widgets.yaml":   {Data: []byte("- greeting:\n    text: \"engine @ENGINE_VERSION@\"\n    href: \"@ENGINE_URL@\"\n")},
	"homepage/bookmarks.yaml": {Data: []byte("[]\n")},
	"homepage/custom.css":     {Data: []byte("body {}\n")},
}

func fixture(t *testing.T, role string) *installation.Installation {
	t.Helper()
	root := t.TempDir()
	i := &installation.Installation{Name: "gorgon", Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state"), Settings: map[string]string{}}
	require.NoError(t, os.MkdirAll(filepath.Join(i.Config, "homepage"), 0o755))
	if role == "main" {
		require.NoError(t, os.MkdirAll(i.Data, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(i.Data, ".backup-main"), nil, 0o644))
	}
	return i
}

func read(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(text)
}

func TestDraw(t *testing.T) {
	inputs := Inputs{Network: "gorgon.local", Version: "v0.9.1", EngineURL: "", ShortHost: "pi2", HealthchecksKey: "k"}
	type Given struct {
		role     string
		settings string
		checks   map[string]bool
	}
	type Then struct {
		settings string
		services string
		widgets  string
	}
	tests := map[string]struct {
		Given Given
		Then  Then
	}{
		"the default page on the main": {
			Given: Given{role: "main", checks: map[string]bool{"gorgon-update": true}},
			Then: Then{
				settings: "title: \"gorgon\"\n",
				services: "- Healthchecks:\n    - Update:\n        description: \"gorgon-update\"\n- Apps:\n    - Home:\n        href: http://gorgon.local\n",
				widgets:  "- greeting:\n    text: \"engine v0.9.1\"\n",
			},
		},
		"a secondary's update check names its host": {
			Given: Given{role: "secondary", checks: map[string]bool{"gorgon-update-pi2": true}},
			Then:  Then{services: "- Healthchecks:\n    - Update:\n        description: \"gorgon-update-pi2\"\n- Apps:\n    - Home:\n        href: http://gorgon.local\n"},
		},
		"the config's file wins": {
			Given: Given{role: "main", settings: "title: Mine\n"},
			Then:  Then{settings: "title: Mine\n"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			i := fixture(t, tt.Given.role)
			if tt.Given.settings != "" {
				require.NoError(t, os.WriteFile(filepath.Join(i.Config, "homepage", "settings.yaml"), []byte(tt.Given.settings), 0o644))
			}
			in := inputs
			in.Role = i.Role()

			_, err := Draw(context.Background(), defaultPage, i, in, checksAnswer{slugs: tt.Given.checks}, &bytes.Buffer{})

			require.NoError(t, err)
			out := filepath.Join(i.State, ".homepage")
			for file, want := range map[string]string{"settings.yaml": tt.Then.settings, "services.yaml": tt.Then.services, "widgets.yaml": tt.Then.widgets} {
				if want != "" {
					assert.Equal(t, want, read(t, filepath.Join(out, file)), file)
				}
			}
			assert.Equal(t, "body {}\n", read(t, filepath.Join(out, "custom.css")))
		})
	}
}

func TestDrawLeavesUnchangedFilesAndHomepagesOwn(t *testing.T) {
	i := fixture(t, "main")
	in := Inputs{Network: "gorgon.local", Version: "v0.9.1", Role: "main"}
	_, err := Draw(context.Background(), defaultPage, i, in, checksAnswer{}, &bytes.Buffer{})
	require.NoError(t, err)
	out := filepath.Join(i.State, ".homepage")
	require.NoError(t, os.WriteFile(filepath.Join(out, "docker.yaml"), []byte("own\n"), 0o644))
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(out, "settings.yaml"), past, past))

	_, err = Draw(context.Background(), defaultPage, i, in, checksAnswer{}, &bytes.Buffer{})

	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(out, "settings.yaml"))
	require.NoError(t, err)
	assert.WithinDuration(t, past, info.ModTime(), time.Second)
	assert.Equal(t, "own\n", read(t, filepath.Join(out, "docker.yaml")))
}
