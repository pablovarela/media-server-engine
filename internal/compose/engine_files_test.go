package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/pablovarela/media-server-engine/internal/installation"
)

type shipped struct {
	i *installation.Installation
}

func shippedInstallation(t *testing.T, settings map[string]string) shipped {
	t.Helper()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	root := t.TempDir()
	i := &installation.Installation{Name: "home", Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), State: filepath.Join(root, "state"), Settings: settings}
	require.NoError(t, os.MkdirAll(i.Config, 0o755))
	for _, file := range []string{"images.yml"} {
		content, err := os.ReadFile(filepath.Join("..", "..", "config-template", file))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(i.Config, file), content, 0o644))
	}
	require.NoError(t, Prepare(os.DirFS(filepath.Join("..", "..")), i.State))
	secrets := filepath.Join(i.State, ".secrets")
	require.NoError(t, os.MkdirAll(secrets, 0o700))
	for _, file := range []string{"vpn.env", "gluetun.env", "sonarr.env", "radarr.env", "prowlarr.env", "portainer_admin", "homepage.env", "apps.env"} {
		require.NoError(t, os.WriteFile(filepath.Join(secrets, file), nil, 0o600))
	}
	return shipped{i: i}
}

func (s shipped) secret(t *testing.T, file, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(s.i.State, ".secrets", file), []byte(content), 0o600))
}

func (s shipped) unpinHomepage(t *testing.T) {
	t.Helper()
	path := filepath.Join(s.i.Config, Stack.ImagesFile)
	text, err := os.ReadFile(path)
	require.NoError(t, err)
	var images map[string]map[string]any
	require.NoError(t, yaml.Unmarshal(text, &images))
	require.Contains(t, images["services"], "homepage")
	delete(images["services"], "homepage")
	text, err = yaml.Marshal(images)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, text, 0o644))
}

func (s shipped) load(t *testing.T, kind Kind, wiring bool) types.Services {
	t.Helper()
	profiles, err := Profiles(s.i, kind, wiring)
	require.NoError(t, err)
	runner, err := NewRunner(os.Stderr, &Outcomes{})
	require.NoError(t, err)
	project, err := runner.Load(context.Background(), s.i, kind, Variables(s.i, "media.local", 0), profiles)
	require.NoError(t, err)
	return project.Services
}

func binds(service types.ServiceConfig) map[string]types.ServiceVolumeConfig {
	mounts := map[string]types.ServiceVolumeConfig{}
	for _, v := range service.Volumes {
		if v.Type == types.VolumeTypeBind {
			mounts[v.Target] = v
		}
	}
	return mounts
}

func environment(t *testing.T, service types.ServiceConfig, key string) string {
	t.Helper()
	value := service.Environment[key]
	require.NotNil(t, value, "%s has no %s", service.Name, key)
	return *value
}

type shippedCheck func(t *testing.T, i *installation.Installation, services types.Services)

func takesItsAPIKeyFromItsOwnSecretsFile(app, key string) shippedCheck {
	return func(t *testing.T, _ *installation.Installation, services types.Services) {
		assert.Equal(t, key, environment(t, services[app], strings.ToUpper(app)+"__AUTH__APIKEY"))
	}
}

func skipsItsLoginOnTheLocalNetwork(app string) shippedCheck {
	return func(t *testing.T, _ *installation.Installation, services types.Services) {
		prefix := strings.ToUpper(app) + "__AUTH__"
		assert.Equal(t, "Forms", environment(t, services[app], prefix+"METHOD"))
		assert.Equal(t, "DisabledForLocalAddresses", environment(t, services[app], prefix+"REQUIRED"))
	}
}

func waitsUntilHealthy(app string) shippedCheck {
	return func(t *testing.T, _ *installation.Installation, services types.Services) {
		assert.Equal(t, types.ServiceConditionHealthy, services["configarr"].DependsOn[app].Condition)
		assert.NotNil(t, services[app].HealthCheck)
	}
}

func mountsTheSharedDataFolder(app string) shippedCheck {
	return func(t *testing.T, i *installation.Installation, services types.Services) {
		assert.Equal(t, filepath.Join(i.Data, "data"), binds(services[app])["/data"].Source)
	}
}

func pinsEveryServiceToADigest(t *testing.T, _ *installation.Installation, services types.Services) {
	require.NotEmpty(t, services)
	for name, service := range services {
		assert.Contains(t, service.Image, "@sha256:", name)
	}
}

func keepsAppStateMediaAndDownloadsUnderTheDataFolder(t *testing.T, i *installation.Installation, services types.Services) {
	for name, service := range services {
		for target, mount := range binds(service) {
			top := strings.Split(target, "/")[1]
			if strings.Contains(mount.Source, "/volumes/") || top == "data" || top == "media" {
				assert.True(t, strings.HasPrefix(mount.Source, i.Data), "%s %s from %s", name, target, mount.Source)
			}
		}
	}
}

func writesAppStateOnlyAsUID1000(t *testing.T, i *installation.Installation, services types.Services) {
	volumes := filepath.Join(i.Data, "volumes") + string(filepath.Separator)
	for name, service := range services {
		writes := false
		for _, mount := range binds(service) {
			writes = writes || strings.HasPrefix(mount.Source, volumes)
		}
		if !writes {
			continue
		}
		env := service.Environment
		asUser := service.User == "1000:1000"
		asPUID := env["PUID"] != nil && *env["PUID"] == "1000" && env["PGID"] != nil && *env["PGID"] == "1000"
		assert.True(t, asUser || asPUID, name)
	}
}

func runsEveryZonedAppInEuropeLondon(t *testing.T, _ *installation.Installation, services types.Services) {
	zoned := 0
	for name, service := range services {
		if tz := service.Environment["TZ"]; tz != nil {
			zoned++
			assert.Equal(t, "Europe/London", *tz, name)
		}
	}
	assert.Positive(t, zoned)
}

func servesTheMediaReadOnlyOnPort80(t *testing.T, i *installation.Installation, services types.Services) {
	require.Contains(t, services, "homepage")
	homepage := services["homepage"]
	mounts := binds(homepage)
	require.NotEmpty(t, homepage.Ports)
	assert.Equal(t, "80", homepage.Ports[0].Published)
	assert.Equal(t, uint32(3000), homepage.Ports[0].Target)
	assert.Equal(t, "1000", environment(t, homepage, "PUID"))
	assert.Equal(t, "1000", environment(t, homepage, "PGID"))
	assert.Equal(t, i.HomepageAllowedHosts("media.local"), environment(t, homepage, "HOMEPAGE_ALLOWED_HOSTS"))
	assert.Equal(t, "stdout", environment(t, homepage, "LOG_TARGETS"))
	assert.Equal(t, filepath.Join(i.State, ".homepage"), mounts["/app/config"].Source)
	assert.Equal(t, filepath.Join(i.Data, "data", "media"), mounts["/media"].Source)
	assert.True(t, mounts["/media"].ReadOnly)
	assert.Equal(t, filepath.Join(i.State, ".homepage-images"), mounts["/app/public/images"].Source)
	assert.True(t, mounts["/app/public/images"].ReadOnly)
}

func mountsNoSeparateDownloadOrMediaFolders(t *testing.T, _ *installation.Installation, services types.Services) {
	separate := []string{"/downloads", "/movies", "/tv", "/data/movies", "/data/tvshows"}
	for name, service := range services {
		for _, volume := range service.Volumes {
			assert.NotContains(t, separate, volume.Target, name)
		}
	}
}

func TestTheShippedComposeFiles(t *testing.T) {
	type Given struct {
		settings         map[string]string
		secrets          map[string]string
		homepageUnpinned bool
	}
	type When struct {
		kind   Kind
		wiring bool
	}
	tests := map[string]struct {
		Given Given
		When  When
		Then  shippedCheck
	}{
		"configarr is left out of a plain apply": {
			When: When{kind: Stack},
			Then: func(t *testing.T, _ *installation.Installation, services types.Services) {
				assert.NotContains(t, services, "configarr")
			},
		},
		"configarr runs only as a wiring step": {
			When: When{kind: Stack, wiring: true},
			Then: func(t *testing.T, _ *installation.Installation, services types.Services) {
				assert.Equal(t, []string{"wiring"}, services["configarr"].Profiles)
			},
		},
		"configarr reads its config from the config repository, read-only": {
			When: When{kind: Stack, wiring: true},
			Then: func(t *testing.T, i *installation.Installation, services types.Services) {
				mount := binds(services["configarr"])["/app/config"]
				assert.Equal(t, filepath.Join(i.Config, "configarr"), mount.Source)
				assert.True(t, mount.ReadOnly)
			},
		},
		"configarr waits until sonarr is healthy": {
			When: When{kind: Stack, wiring: true},
			Then: waitsUntilHealthy("sonarr"),
		},
		"configarr waits until radarr is healthy": {
			When: When{kind: Stack, wiring: true},
			Then: waitsUntilHealthy("radarr"),
		},
		"the template pins every stack service to a digest": {
			When: When{kind: Stack, wiring: true},
			Then: pinsEveryServiceToADigest,
		},
		"app state, media and downloads live under the data folder": {
			When: When{kind: Stack, wiring: true},
			Then: keepsAppStateMediaAndDownloadsUnderTheDataFolder,
		},
		"every service that writes app state runs as uid 1000": {
			When: When{kind: Stack, wiring: true},
			Then: writesAppStateOnlyAsUID1000,
		},
		"sonarr takes its API key from its own secrets file": {
			Given: Given{secrets: map[string]string{"sonarr.env": "SONARR__AUTH__APIKEY=sk\n"}},
			When:  When{kind: Stack},
			Then:  takesItsAPIKeyFromItsOwnSecretsFile("sonarr", "sk"),
		},
		"radarr takes its API key from its own secrets file, not sonarr's": {
			Given: Given{secrets: map[string]string{"sonarr.env": "SONARR__AUTH__APIKEY=sk\n", "radarr.env": "RADARR__AUTH__APIKEY=rk\n"}},
			When:  When{kind: Stack},
			Then: func(t *testing.T, i *installation.Installation, services types.Services) {
				takesItsAPIKeyFromItsOwnSecretsFile("radarr", "rk")(t, i, services)
				assert.NotContains(t, services["radarr"].Environment, "SONARR__AUTH__APIKEY")
			},
		},
		"prowlarr takes its API key from its own secrets file": {
			Given: Given{secrets: map[string]string{"prowlarr.env": "PROWLARR__AUTH__APIKEY=pk\n"}},
			When:  When{kind: Stack},
			Then:  takesItsAPIKeyFromItsOwnSecretsFile("prowlarr", "pk"),
		},
		"sonarr skips its login on the local network": {
			When: When{kind: Stack},
			Then: skipsItsLoginOnTheLocalNetwork("sonarr"),
		},
		"radarr skips its login on the local network": {
			When: When{kind: Stack},
			Then: skipsItsLoginOnTheLocalNetwork("radarr"),
		},
		"prowlarr skips its login on the local network": {
			When: When{kind: Stack},
			Then: skipsItsLoginOnTheLocalNetwork("prowlarr"),
		},
		"portainer creates its admin from a read-only password file": {
			When: When{kind: Stack},
			Then: func(t *testing.T, i *installation.Installation, services types.Services) {
				mount := binds(services["portainer"])["/run/secrets/portainer_admin"]
				assert.Equal(t, types.ShellCommand{"--admin-password-file", "/run/secrets/portainer_admin"}, services["portainer"].Command)
				assert.Equal(t, filepath.Join(i.State, ".secrets", "portainer_admin"), mount.Source)
				assert.True(t, mount.ReadOnly)
			},
		},
		"gluetun takes its control key from its own secrets file": {
			Given: Given{secrets: map[string]string{"gluetun.env": `HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"k1"}` + "\n"}},
			When:  When{kind: Stack},
			Then: func(t *testing.T, _ *installation.Installation, services types.Services) {
				assert.Equal(t, `{"auth":"apikey","apikey":"k1"}`, environment(t, services["gluetun"], "HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE"))
			},
		},
		"every app with a time zone runs in the installation's": {
			Given: Given{settings: map[string]string{"TZ": "Europe/London"}},
			When:  When{kind: Stack},
			Then:  runsEveryZonedAppInEuropeLondon,
		},
		"jellyfin runs in UTC when the installation sets no time zone": {
			When: When{kind: Stack},
			Then: func(t *testing.T, _ *installation.Installation, services types.Services) {
				assert.Equal(t, "Etc/UTC", environment(t, services["jellyfin"], "TZ"))
			},
		},
		"the landing page serves the media read-only on port 80": {
			When: When{kind: Stack},
			Then: servesTheMediaReadOnlyOnPort80,
		},
		"the landing page moves to the installation's port": {
			Given: Given{settings: map[string]string{"HOMEPAGE_PORT": "8080"}},
			When:  When{kind: Stack},
			Then: func(t *testing.T, i *installation.Installation, services types.Services) {
				require.NotEmpty(t, services["homepage"].Ports)
				assert.Equal(t, "8080", services["homepage"].Ports[0].Published)
				assert.Equal(t, i.HomepageAllowedHosts("media.local"), environment(t, services["homepage"], "HOMEPAGE_ALLOWED_HOSTS"))
			},
		},
		"the landing page is left out when the images file does not pin it": {
			Given: Given{homepageUnpinned: true},
			When:  When{kind: Stack},
			Then: func(t *testing.T, _ *installation.Installation, services types.Services) {
				assert.NotContains(t, services, "homepage")
				assert.Contains(t, services, "jellyfin")
			},
		},
		"deluge mounts the shared data folder": {
			When: When{kind: Stack},
			Then: mountsTheSharedDataFolder("deluge"),
		},
		"radarr mounts the shared data folder": {
			When: When{kind: Stack},
			Then: mountsTheSharedDataFolder("radarr"),
		},
		"sonarr mounts the shared data folder": {
			When: When{kind: Stack},
			Then: mountsTheSharedDataFolder("sonarr"),
		},
		"bazarr mounts the shared data folder": {
			When: When{kind: Stack},
			Then: mountsTheSharedDataFolder("bazarr"),
		},
		"jellyfin mounts the shared data folder": {
			When: When{kind: Stack},
			Then: mountsTheSharedDataFolder("jellyfin"),
		},
		"no service mounts separate download or media folders": {
			When: When{kind: Stack, wiring: true},
			Then: mountsNoSeparateDownloadOrMediaFolders,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			settings := map[string]string{}
			for key, value := range tt.Given.settings {
				settings[key] = value
			}
			s := shippedInstallation(t, settings)
			for file, content := range tt.Given.secrets {
				s.secret(t, file, content)
			}
			if tt.Given.homepageUnpinned {
				s.unpinHomepage(t)
			}

			services := s.load(t, tt.When.kind, tt.When.wiring)

			tt.Then(t, s.i, services)
		})
	}
}

func TestTheEngineComposeFilesCarryNoImageVersions(t *testing.T) {
	type Given struct {
		file string
	}
	tests := map[string]struct {
		Given Given
	}{
		"the stack": {Given: Given{file: "docker-compose.yml"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "..", tt.Given.file))
			require.NoError(t, err)

			assert.NotRegexp(t, `(?m)^\s+image:`, string(content))
		})
	}
}
