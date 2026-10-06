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
	for _, file := range []string{"images.yml", "images.monitoring.yml"} {
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

func (s shipped) load(t *testing.T, kind Kind, profiles ...string) *types.Project {
	t.Helper()
	runner, err := NewRunner(os.Stderr, &Outcomes{})
	require.NoError(t, err)
	project, err := runner.Load(context.Background(), s.i, kind, Variables(s.i, "media.local", 0), profiles)
	require.NoError(t, err)
	return project
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

func TestConfigarrOnlyRunsAsAWiringStep(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})

	assert.NotContains(t, s.load(t, Stack, "homepage").Services, "configarr")
	wiring := s.load(t, Stack, "homepage", "wiring")
	assert.Equal(t, []string{"wiring"}, wiring.Services["configarr"].Profiles)
}

func TestTheEngineComposeFilesCarryNoImageVersions(t *testing.T) {
	for _, file := range []string{"docker-compose.yml", "docker-compose.monitoring.yml"} {
		content, err := os.ReadFile(filepath.Join("..", "..", file))
		require.NoError(t, err)
		for _, line := range strings.Split(string(content), "\n") {
			assert.NotRegexp(t, `^\s+image:`, line, file)
		}
	}
}

func TestTheTemplatePinsEveryServiceToADigest(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	for _, project := range []*types.Project{s.load(t, Stack, "homepage", "wiring"), s.load(t, Monitoring)} {
		for name, service := range project.Services {
			assert.Contains(t, service.Image, "@sha256:", name)
		}
	}
}

func TestAppStateMediaAndDownloadsLiveUnderTheDataFolder(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	for name, service := range s.load(t, Stack, "homepage").Services {
		for target, mount := range binds(service) {
			top := strings.Split(target, "/")[1]
			if strings.Contains(mount.Source, "/volumes/") || top == "data" || top == "media" {
				assert.True(t, strings.HasPrefix(mount.Source, s.i.Data), "%s %s from %s", name, target, mount.Source)
			}
		}
	}
}

func TestEveryServiceThatWritesAppStateRunsAsUID1000(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	volumes := filepath.Join(s.i.Data, "volumes") + string(filepath.Separator)
	for name, service := range s.load(t, Stack, "homepage").Services {
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

func TestConfigarrReadsItsConfigFromTheConfigRepoReadOnly(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	mount := binds(s.load(t, Stack, "homepage", "wiring").Services["configarr"])["/app/config"]

	assert.Equal(t, filepath.Join(s.i.Config, "configarr"), mount.Source)
	assert.True(t, mount.ReadOnly)
}

func TestConfigarrWaitsUntilSonarrAndRadarrAreHealthy(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	services := s.load(t, Stack, "homepage", "wiring").Services
	for _, app := range []string{"sonarr", "radarr"} {
		assert.Equal(t, types.ServiceConditionHealthy, services["configarr"].DependsOn[app].Condition, app)
		assert.NotNil(t, services[app].HealthCheck, app)
	}
}

func TestTheArrsTakeTheirAPIKeyFromTheirOwnSecretsFile(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	s.secret(t, "sonarr.env", "SONARR__AUTH__APIKEY=sk\n")
	s.secret(t, "radarr.env", "RADARR__AUTH__APIKEY=rk\n")
	s.secret(t, "prowlarr.env", "PROWLARR__AUTH__APIKEY=pk\n")
	services := s.load(t, Stack, "homepage").Services
	for app, key := range map[string]string{"sonarr": "sk", "radarr": "rk", "prowlarr": "pk"} {
		value := services[app].Environment[strings.ToUpper(app)+"__AUTH__APIKEY"]
		require.NotNil(t, value, app)
		assert.Equal(t, key, *value, app)
	}
	assert.NotContains(t, services["radarr"].Environment, "SONARR__AUTH__APIKEY")
}

func TestPortainerCreatesItsAdminFromAReadOnlyPasswordFile(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	portainer := s.load(t, Stack, "homepage").Services["portainer"]
	mount := binds(portainer)["/run/secrets/portainer_admin"]

	assert.Equal(t, types.ShellCommand{"--admin-password-file", "/run/secrets/portainer_admin"}, portainer.Command)
	assert.Equal(t, filepath.Join(s.i.State, ".secrets", "portainer_admin"), mount.Source)
	assert.True(t, mount.ReadOnly)
}

func TestTheArrsSkipTheirLoginOnTheLocalNetwork(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	services := s.load(t, Stack, "homepage").Services
	for _, app := range []string{"sonarr", "radarr", "prowlarr"} {
		env := services[app].Environment
		prefix := strings.ToUpper(app) + "__AUTH__"
		require.NotNil(t, env[prefix+"METHOD"], app)
		require.NotNil(t, env[prefix+"REQUIRED"], app)
		assert.Equal(t, "Forms", *env[prefix+"METHOD"], app)
		assert.Equal(t, "DisabledForLocalAddresses", *env[prefix+"REQUIRED"], app)
	}
}

func TestEveryAppRunsInTheInstallationsTimeZone(t *testing.T) {
	zoned := shippedInstallation(t, map[string]string{"TZ": "Europe/London"})
	for name, service := range zoned.load(t, Stack, "homepage").Services {
		if tz := service.Environment["TZ"]; tz != nil {
			assert.Equal(t, "Europe/London", *tz, name)
		}
	}
	jellyfin := shippedInstallation(t, map[string]string{}).load(t, Stack, "homepage").Services["jellyfin"]
	require.NotNil(t, jellyfin.Environment["TZ"])
	assert.Equal(t, "Etc/UTC", *jellyfin.Environment["TZ"])
}

func TestTheLandingPage(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	homepage := s.load(t, Stack, "homepage").Services["homepage"]
	mounts := binds(homepage)

	require.NotEmpty(t, homepage.Ports)
	assert.Equal(t, "80", homepage.Ports[0].Published)
	assert.Equal(t, uint32(3000), homepage.Ports[0].Target)
	assert.Equal(t, "1000", *homepage.Environment["PUID"])
	assert.Equal(t, "1000", *homepage.Environment["PGID"])
	assert.Contains(t, *homepage.Environment["HOMEPAGE_ALLOWED_HOSTS"], "media.local")
	assert.Equal(t, "stdout", *homepage.Environment["LOG_TARGETS"])
	assert.Equal(t, filepath.Join(s.i.State, ".homepage"), mounts["/app/config"].Source)
	assert.Equal(t, filepath.Join(s.i.Data, "data", "media"), mounts["/media"].Source)
	assert.True(t, mounts["/media"].ReadOnly)
	assert.Equal(t, filepath.Join(s.i.State, ".homepage-images"), mounts["/app/public/images"].Source)
	assert.True(t, mounts["/app/public/images"].ReadOnly)

	moved := shippedInstallation(t, map[string]string{"HOMEPAGE_PORT": "8080"})
	assert.Equal(t, "8080", moved.load(t, Stack, "homepage").Services["homepage"].Ports[0].Published)
	assert.NotContains(t, s.load(t, Stack).Services, "homepage", "without the homepage profile")
}

func TestGluetunTakesItsControlKeyFromItsOwnSecretsFile(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	s.secret(t, "gluetun.env", `HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"k1"}`+"\n")
	role := s.load(t, Stack, "homepage").Services["gluetun"].Environment["HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE"]

	require.NotNil(t, role)
	assert.Equal(t, `{"auth":"apikey","apikey":"k1"}`, *role)
}

func TestDownloadsAndMediaShareOneDataMount(t *testing.T) {
	s := shippedInstallation(t, map[string]string{})
	services := s.load(t, Stack, "homepage").Services
	for _, name := range []string{"deluge", "radarr", "sonarr", "bazarr", "jellyfin"} {
		assert.Equal(t, filepath.Join(s.i.Data, "data"), binds(services[name])["/data"].Source, name)
	}
	old := []string{"/downloads", "/movies", "/tv", "/data/movies", "/data/tvshows"}
	for name, service := range services {
		for target := range binds(service) {
			assert.NotContains(t, old, target, name)
		}
	}
}
