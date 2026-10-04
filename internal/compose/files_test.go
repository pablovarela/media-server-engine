package compose

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

var engineFiles = fstest.MapFS{
	"docker-compose.yml":            {Data: []byte("services: {}\n")},
	"docker-compose.monitoring.yml": {Data: []byte("services: {}\n")},
	"grafana/datasources/a.yml":     {Data: []byte("a\n")},
	"prometheus/prometheus.yml":     {Data: []byte("p\n")},
	"README.md":                     {Data: []byte("not an engine file\n")},
}

func TestPrepare(t *testing.T) {
	state := t.TempDir()

	require.NoError(t, Prepare(engineFiles, state))

	for _, file := range []string{"docker-compose.yml", "docker-compose.monitoring.yml", "grafana/datasources/a.yml", "prometheus/prometheus.yml"} {
		got, err := os.ReadFile(filepath.Join(state, file))
		require.NoError(t, err, file)
		assert.Equal(t, string(engineFiles[file].Data), string(got), file)
	}
	assert.NoFileExists(t, filepath.Join(state, "README.md"))

	t.Run("unchanged files are left alone", func(t *testing.T) {
		path := filepath.Join(state, "prometheus", "prometheus.yml")
		past := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(path, past, past))

		require.NoError(t, Prepare(engineFiles, state))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.WithinDuration(t, past, info.ModTime(), time.Second)
	})
}

func TestVariables(t *testing.T) {
	i := &installation.Installation{Config: "/c", Data: "/d", Settings: map[string]string{"TZ": "Europe/London", "HOMEPAGE_PORT": "8080"}}

	assert.Equal(t, []string{
		"DATA_DIR=/d",
		"CONFIG_DIR=/c",
		"TZ=Europe/London",
		"HOMEPAGE_PORT=8080",
		"HOMEPAGE_ALLOWED_HOSTS=gorgon.local:8080,localhost:8080,127.0.0.1:8080",
		"DOCKER_GID=998",
		"COMPOSE_PROFILES=",
	}, Variables(i, "gorgon.local", 998))

	assert.Contains(t, Variables(&installation.Installation{Settings: map[string]string{}}, "x.local", 0), "TZ=Etc/UTC")
}

func TestProfiles(t *testing.T) {
	type Given struct {
		images string
	}
	type When struct {
		kind   Kind
		wiring bool
	}
	type Then struct {
		profiles []string
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"homepage pinned":            {Given: Given{images: "services:\n  homepage:\n    image: ghcr.io/gethomepage/homepage@sha256:abc\n"}, When: When{kind: Stack}, Then: Then{profiles: []string{"homepage"}}},
		"homepage not pinned":        {Given: Given{images: "services:\n  jellyfin:\n    image: x\n"}, When: When{kind: Stack}, Then: Then{profiles: nil}},
		"wiring requested":           {Given: Given{images: "services: {}\n"}, When: When{kind: Stack, wiring: true}, Then: Then{profiles: []string{"wiring"}}},
		"monitoring has no profiles": {Given: Given{images: "services:\n  homepage:\n    image: x\n"}, When: When{kind: Monitoring}, Then: Then{profiles: nil}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			config := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(config, "images.yml"), []byte(tt.Given.images), 0o644))

			profiles, err := Profiles(&installation.Installation{Config: config}, tt.When.kind, tt.When.wiring)

			require.NoError(t, err)
			assert.Equal(t, tt.Then.profiles, profiles)
		})
	}
}
