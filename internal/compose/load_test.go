package compose

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

func fixtureInstallation(t *testing.T, images, override string) *installation.Installation {
	t.Helper()
	root := t.TempDir()
	i := &installation.Installation{Name: "gorgon", Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state"), Settings: map[string]string{}}
	require.NoError(t, os.MkdirAll(i.Config, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(i.Config, "images.yml"), []byte(images), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(i.Config, "images.monitoring.yml"), []byte("services:\n  prometheus:\n    image: prom@sha256:p\n"), 0o644))
	if override != "" {
		require.NoError(t, os.WriteFile(filepath.Join(i.Config, "compose.override.yml"), []byte(override), 0o644))
	}
	require.NoError(t, Prepare(os.DirFS("testdata/engine"), i.State))
	require.NoError(t, os.MkdirAll(filepath.Join(i.State, ".secrets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(i.State, ".secrets", "apps.env"), []byte("A=1\n"), 0o600))
	return i
}

func TestLoad(t *testing.T) {
	images := "services:\n  jellyfin:\n    image: jellyfin@sha256:j\n  homepage:\n    image: homepage@sha256:h\n  configarr:\n    image: configarr@sha256:c\n  portainer:\n    image: portainer@sha256:p\n"
	type Given struct {
		override string
		shell    map[string]string
	}
	type When struct {
		kind     Kind
		profiles []string
	}
	type Then struct {
		services []string
		check    func(t *testing.T, i *installation.Installation, project projectView)
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  Then
	}{
		"the stack with its images, variables and paths": {
			When: When{kind: Stack, profiles: []string{"homepage"}},
			Then: Then{services: []string{"homepage", "jellyfin", "portainer"}, check: func(t *testing.T, i *installation.Installation, p projectView) {
				assert.Equal(t, "media-server", p.name)
				assert.Equal(t, "jellyfin@sha256:j", p.images["jellyfin"])
				assert.Equal(t, filepath.Join(i.Data, "volumes", "jellyfin"), p.volumeSources["jellyfin"][0])
				assert.Equal(t, "Europe/London", p.environment["jellyfin"]["TZ"])
				assert.Equal(t, "1", p.environment["jellyfin"]["A"], "env_file read from the state directory")
			}},
		},
		"wiring adds configarr": {
			When: When{kind: Stack, profiles: []string{"homepage", "wiring"}},
			Then: Then{services: []string{"configarr", "homepage", "jellyfin", "portainer"}},
		},
		"the override is merged": {
			Given: Given{override: "services:\n  jellyfin:\n    environment:\n      EXTRA: yes\n"},
			When:  When{kind: Stack},
			Then: Then{services: []string{"jellyfin", "portainer"}, check: func(t *testing.T, _ *installation.Installation, p projectView) {
				assert.Equal(t, "yes", p.environment["jellyfin"]["EXTRA"])
			}},
		},
		"the shell's variables do not leak in": {
			Given: Given{shell: map[string]string{"DATA_DIR": "/elsewhere", "CONFIG_DIR": "/elsewhere", "COMPOSE_PROFILES": "wiring"}},
			When:  When{kind: Stack},
			Then: Then{services: []string{"jellyfin", "portainer"}, check: func(t *testing.T, i *installation.Installation, p projectView) {
				assert.Equal(t, filepath.Join(i.Data, "volumes", "jellyfin"), p.volumeSources["jellyfin"][0])
				assert.NotContains(t, p.services, "configarr")
			}},
		},
		"monitoring": {
			When: When{kind: Monitoring},
			Then: Then{services: []string{"prometheus"}, check: func(t *testing.T, i *installation.Installation, p projectView) {
				assert.Equal(t, "monitoring", p.name)
				assert.Equal(t, filepath.Join(i.State, "prometheus"), p.volumeSources["prometheus"][0])
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DOCKER_CONFIG", t.TempDir())
			for key, value := range tt.Given.shell {
				t.Setenv(key, value)
			}
			i := fixtureInstallation(t, images, tt.Given.override)
			i.Settings["TZ"] = "Europe/London"
			runner, err := NewRunner(os.Stderr, &Outcomes{})
			require.NoError(t, err)

			project, err := runner.Load(context.Background(), i, tt.When.kind, Variables(i, "gorgon.local", 998), tt.When.profiles)

			require.NoError(t, err)
			view := viewOf(project)
			assert.Equal(t, tt.Then.services, view.services)
			if tt.Then.check != nil {
				tt.Then.check(t, i, view)
			}
		})
	}
}

type projectView struct {
	name          string
	services      []string
	images        map[string]string
	volumeSources map[string][]string
	environment   map[string]map[string]string
}

func viewOf(project *types.Project) projectView {
	view := projectView{
		name:          project.Name,
		images:        map[string]string{},
		volumeSources: map[string][]string{},
		environment:   map[string]map[string]string{},
	}
	for name, service := range project.Services {
		view.services = append(view.services, name)
		view.images[name] = service.Image
		for _, volume := range service.Volumes {
			view.volumeSources[name] = append(view.volumeSources[name], volume.Source)
		}
		view.environment[name] = map[string]string{}
		for key, value := range service.Environment {
			if value != nil {
				view.environment[name][key] = *value
			}
		}
	}
	sort.Strings(view.services)
	return view
}
