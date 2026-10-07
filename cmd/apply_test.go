package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/compose"
	"github.com/pablovarela/media-server-engine/internal/images"
	"github.com/pablovarela/media-server-engine/internal/installation"
	"github.com/pablovarela/media-server-engine/internal/process"
	"github.com/pablovarela/media-server-engine/internal/version"
	"github.com/pablovarela/media-server-engine/internal/wiring"
)

type noImages struct{}

func (noImages) ImageList(context.Context, client.ImageListOptions) (client.ImageListResult, error) {
	return client.ImageListResult{}, nil
}

func (noImages) ImageRemove(context.Context, string, client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	return client.ImageRemoveResult{}, nil
}

type applyFixture struct {
	home     string
	data     string
	composer *mockComposeRunner
	runner   *mockCommandRunner
	project  *types.Project
	requests *[]string
}

func newApplyFixture(t *testing.T) applyFixture {
	t.Helper()
	_, home := xdgHome(t, map[string]string{"gorgon": "INSTALLATION_NAME=gorgon\nRESTIC_REPOSITORY=b2:bucket\n"})
	config := filepath.Join(home, ".config", "mse", "gorgon")
	require.NoError(t, os.WriteFile(filepath.Join(config, "secrets", "healthchecks.sops.env"), nil, 0o644))
	data := filepath.Join(home, ".local", "share", "mse", "gorgon")
	require.NoError(t, os.MkdirAll(data, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, ".backup-main"), nil, 0o644))
	return applyFixture{
		home: home, data: data,
		composer: newMockComposeRunner(t), runner: newMockCommandRunner(t),
		project:  &types.Project{Name: "media-server", Services: types.Services{"gluetun": {Name: "gluetun"}}},
		requests: &[]string{},
	}
}

func (f applyFixture) deps(t *testing.T, systemd bool) Dependencies {
	t.Helper()
	return Dependencies{
		ResticBinary: localRestic,
		Environment: func(key string) string {
			if key == "USER" {
				return "pablo"
			}
			return ""
		},
		Home: f.home, Update: newMockUpdater(t),
		Decrypt: func(string) ([]byte, error) {
			return []byte("HEALTHCHECKS_PING_KEY=ping-key\nHEALTHCHECKS_MANAGE_KEY=manage-key\n"), nil
		},
		Host: installation.Host{GOOS: "linux", Hostname: func() (string, error) { return "gorgon.local", nil }},
		Engine: fstest.MapFS{
			"docker-compose.yml":      {Data: []byte("services: {}\n")},
			"homepage/settings.yaml":  {Data: []byte("title: gorgon\n")},
			"homepage/services.yaml":  {Data: []byte("[]\n")},
			"homepage/widgets.yaml":   {Data: []byte("[]\n")},
			"homepage/bookmarks.yaml": {Data: []byte("[]\n")},
			"homepage/custom.css":     {Data: []byte("")},
		},
		Compose: func(io.Writer, *compose.Outcomes) (composeRunner, error) { return f.composer, nil },
		Run:     func(_, _ io.Writer) commandRunner { return f.runner },
		Images:  func() (images.Client, error) { return noImages{}, nil },
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			*f.requests = append(*f.requests, r.Method+" "+r.URL.Path)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		})},
		Systemd:   func() bool { return systemd },
		LocalTime: filepath.Join(t.TempDir(), "localtime"),
		Now:       func() time.Time { return time.Date(2026, 10, 6, 5, 0, 0, 0, time.UTC) },
		Sleep:     func(time.Duration) { t.Fatal("nothing needs a retry") },
	}
}

func (f applyFixture) expectApply(pullErr error) {
	f.composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, []string{"homepage"}).Return(f.project, nil)
	f.composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, []string{"homepage", "wiring"}).Return(f.project, nil)
	f.composer.EXPECT().Pull(mock.Anything, f.project).Return(compose.Pulled{New: 2, Total: 13}, pullErr)
	if pullErr != nil {
		return
	}
	f.composer.EXPECT().Up(mock.Anything, f.project, []string(nil), compose.NoWait).Return(nil)
	f.composer.EXPECT().Detached(mock.Anything, f.project, "gluetun", gluetunDependents).Return(nil, nil)
	f.composer.EXPECT().Ps(mock.Anything, f.project).Return(nil, nil)
}

func TestApplyCommand(t *testing.T) {
	type Given struct {
		systemd bool
		pullErr error
	}
	type Then struct {
		code     int
		requests []string
		stdout   []string
	}
	tests := map[string]struct {
		Given Given
		When  []string
		Then  Then
	}{
		"applies and sets up the checks under systemd": {
			Given: Given{systemd: true},
			When:  []string{"apply"},
			Then: Then{
				requests: []string{"POST /api/v3/checks/", "POST /api/v3/checks/", "POST /api/v3/checks/"},
				stdout: []string{
					"Setting up the Healthchecks checks... gorgon-backup, gorgon-verify, gorgon-update.\n",
					"Pulling images... pulled 2 new images, 11 up to date.\n", "Starting the stack...", "Reattaching to gluetun... nothing to reattach.\n",
					"Reloading Homepage... not running.\n", "Removing outdated images...",
				},
			},
		},
		"no checks and no pings without systemd": {
			When: []string{"apply"},
			Then: Then{stdout: []string{"Pulling images..."}},
		},
		"after an update it reports success": {
			When: []string{"apply", "--after-update=a1b2c3", "--verbose"},
			Then: Then{requests: []string{"GET /ping-key/gorgon-update"}},
		},
		"after an update a failure reports fail": {
			Given: Given{pullErr: errors.New("manifest unknown")},
			When:  []string{"apply", "--after-update=a1b2c3"},
			Then:  Then{code: 1, requests: []string{"GET /ping-key/gorgon-update/fail"}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t)
			f.expectApply(tt.Given.pullErr)
			if tt.Given.systemd {
				f.runner.EXPECT().Output(mock.Anything, process.Command{Name: "timedatectl", Args: []string{"show", "-p", "Timezone", "--value"}}).
					Return(process.Result{Stdout: []byte("Europe/London\n")}, nil)
			}
			root := NewRootCommand(f.deps(t, tt.Given.systemd))
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)

			code := run(context.Background(), root, tt.When)

			assert.Equal(t, tt.Then.code, code, stderr.String())
			assert.Equal(t, tt.Then.requests, append([]string(nil), *f.requests...))
			for _, line := range tt.Then.stdout {
				assert.Contains(t, stdout.String(), line)
			}
		})
	}
}

func TestApplyCreatesTheDataFolders(t *testing.T) {
	f := newApplyFixture(t)
	jellyfin := filepath.Join(f.data, "volumes", "jellyfin")
	f.project.Services["jellyfin"] = types.ServiceConfig{Name: "jellyfin", Volumes: []types.ServiceVolumeConfig{{Type: types.VolumeTypeBind, Source: jellyfin}}}
	f.expectApply(nil)
	root := NewRootCommand(f.deps(t, false))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply"})

	require.Equal(t, 0, code, stderr.String())
	assert.DirExists(t, jellyfin)
}

func TestApplyCreatesTheFoldersTheConfigDeclaresInsideTheSharedMount(t *testing.T) {
	f := newApplyFixture(t)
	shared := filepath.Join(f.data, "data")
	f.project.Services["jellyfin"] = types.ServiceConfig{Name: "jellyfin", Volumes: []types.ServiceVolumeConfig{{Type: types.VolumeTypeBind, Source: shared, Target: "/data"}}}
	config := filepath.Join(f.home, ".config", "mse", "gorgon")
	require.NoError(t, os.WriteFile(filepath.Join(config, "apps.yml"), []byte("jellyfin:\n  libraries:\n    - {name: Anime, type: tvshows, path: /data/media/anime}\ndeluge:\n  core: {download_location: /data/downloads/incomplete}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(config, "configarr"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(config, "configarr", "config.yml"), []byte("radarr:\n  main:\n    api_key: !secret RADARR_API_KEY\n    root_folders: [/data/media/movies]\n"), 0o644))
	f.expectApply(nil)
	root := NewRootCommand(f.deps(t, false))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply"})

	require.Equal(t, 0, code, stderr.String())
	for _, folder := range []string{"media/anime", "downloads/incomplete", "media/movies"} {
		assert.DirExists(t, filepath.Join(shared, folder))
	}
}

func TestApplyReattachesTheDetachedServices(t *testing.T) {
	f := newApplyFixture(t)
	f.composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(f.project, nil)
	f.composer.EXPECT().Pull(mock.Anything, f.project).Return(compose.Pulled{}, nil)
	f.composer.EXPECT().Up(mock.Anything, f.project, []string(nil), compose.NoWait).Return(nil)
	f.composer.EXPECT().Detached(mock.Anything, f.project, "gluetun", gluetunDependents).Return([]string{"deluge"}, nil)
	f.composer.EXPECT().Recreate(mock.Anything, f.project, []string{"deluge"}).Return(nil)
	f.composer.EXPECT().Ps(mock.Anything, f.project).Return(nil, nil)
	root := NewRootCommand(f.deps(t, false))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply"})

	require.Equal(t, 0, code, stderr.String())
	assert.Contains(t, stdout.String(), "Reattaching to gluetun... recreated deluge.\n")
}

func TestApplyAfterAnUpdateReportsFailWhenTheConfigNeedsAnotherMajor(t *testing.T) {
	f := newApplyFixture(t)
	writeHealthchecksKeys(t, f)
	require.NoError(t, os.WriteFile(filepath.Join(f.home, ".config", "mse", "gorgon", "config.yml"), []byte("config: 1\n"), 0o644))
	deps := f.deps(t, false)
	deps.Build = version.Build{Version: "v0.16.1"}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply", "--after-update=a1b2c3"})

	assert.Equal(t, 1, code)
	assert.Equal(t, []string{"GET /ping-key/gorgon-update/fail"}, *f.requests)
}

func TestApplyRefusesARunIDThatIsNotOne(t *testing.T) {
	f := newApplyFixture(t)
	root := NewRootCommand(f.deps(t, false))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply", "--after-update=foo"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "--after-update takes the run id of the update that handed over, six hex digits")
	assert.Empty(t, *f.requests)
}

func TestApplyWiresTheAppsAfterStartingThem(t *testing.T) {
	f := newApplyFixture(t)
	f.expectApply(nil)
	f.composer.EXPECT().RunOnce(mock.Anything, f.project, "configarr", mock.Anything).Return(0, nil)
	deps := f.deps(t, false)
	deps.WiringSteps = func(configarr wiring.OneOff, tool io.Writer) ([]wiring.Step, error) {
		return []wiring.Step{
			{Name: "prowlarr", Run: func(_ context.Context, env wiring.Env) error {
				assert.Equal(t, "gorgon", env.Settings["INSTALLATION_NAME"])
				assert.Equal(t, "ping-key", env.Secrets["HEALTHCHECKS_PING_KEY"])
				env.Change("prowlarr", "add tag flaresolverr")
				return nil
			}},
			{Name: "configarr", Run: wiring.Configarr(configarr, tool)},
		}, nil
	}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply"})

	require.Equal(t, 0, code, stderr.String())
	assert.Regexp(t, `(?s)Reattaching to gluetun\.\.\. nothing to reattach\.\nWiring the apps\.\.\.\nprowlarr: add tag flaresolverr\n  1 change\.\nReloading Homepage`, stdout.String())
}

func TestAFailedWiringStepIsNamedAndFailsTheApply(t *testing.T) {
	f := newApplyFixture(t)
	f.expectApply(nil)
	deps := f.deps(t, false)
	deps.WiringSteps = func(wiring.OneOff, io.Writer) ([]wiring.Step, error) {
		return []wiring.Step{{Name: "seerr", Run: func(context.Context, wiring.Env) error { return errors.New("jellyfin has no library Cartoons") }}}, nil
	}
	root := NewRootCommand(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := run(context.Background(), root, []string{"apply"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "seerr: jellyfin has no library Cartoons")
	assert.Contains(t, stderr.String(), "mse: wiring failed: seerr")
	assert.Contains(t, stdout.String(), "Reloading Homepage")
	assert.NotContains(t, stdout.String(), "Removing outdated images")
}

func TestTheWiringWaitsForSlowAppsLongerThanOtherRequests(t *testing.T) {
	f := newApplyFixture(t)
	f.expectApply(nil)
	deps := f.deps(t, false)
	deps.HTTP.Timeout = 30 * time.Second
	deps.WiringSteps = func(wiring.OneOff, io.Writer) ([]wiring.Step, error) {
		return []wiring.Step{{Name: "prowlarr", Run: func(_ context.Context, env wiring.Env) error {
			assert.Zero(t, env.HTTP.Timeout, "each wiring request has its own deadline")
			assert.NotNil(t, env.HTTP.Transport)
			return nil
		}}}, nil
	}
	root := NewRootCommand(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	assert.Equal(t, 0, run(context.Background(), root, []string{"apply"}))
}

func TestAPanicAfterAnUpdateStillReportsFail(t *testing.T) {
	f := newApplyFixture(t)
	writeHealthchecksKeys(t, f)
	f.composer.EXPECT().Load(mock.Anything, mock.Anything, compose.Stack, mock.Anything, mock.Anything).Return(f.project, nil)
	f.composer.EXPECT().Pull(mock.Anything, f.project).Return(compose.Pulled{}, nil)
	f.composer.EXPECT().Up(mock.Anything, f.project, []string(nil), compose.NoWait).Return(nil)
	f.composer.EXPECT().Detached(mock.Anything, f.project, "gluetun", gluetunDependents).Return(nil, nil)
	deps := f.deps(t, false)
	deps.WiringSteps = func(wiring.OneOff, io.Writer) ([]wiring.Step, error) {
		return []wiring.Step{{Name: "seerr", Run: func(context.Context, wiring.Env) error { panic("a bug") }}}, nil
	}
	root := NewRootCommand(deps)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	assert.Panics(t, func() { run(context.Background(), root, []string{"apply", "--after-update=a1b2c3"}) })

	assert.Equal(t, []string{"GET /ping-key/gorgon-update/fail"}, *f.requests)
}

func TestThePullSaysHowManyImagesWereNew(t *testing.T) {
	for pulled, want := range map[compose.Pulled]string{
		{New: 0, Total: 13}: "13 up to date",
		{New: 1, Total: 13}: "pulled 1 new image, 12 up to date",
		{New: 2, Total: 2}:  "pulled 2 new images",
		{New: 0, Total: 0}:  "nothing to pull",
	} {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, describePull(pulled))
		})
	}
}
