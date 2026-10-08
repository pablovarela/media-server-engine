package compose

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/pablovarela/media-server-engine/internal/paint"
)

func TestOperations(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	ctx := context.Background()

	t.Run("up with services touches only those services", func(t *testing.T) {
		full := &types.Project{Name: "media-server", Services: types.Services{
			"jellyfin": {Name: "jellyfin", Image: "j"},
			"sonarr":   {Name: "sonarr", Image: "s"},
		}}
		service := newMockService(t)
		service.EXPECT().Up(ctx, mock.MatchedBy(func(p *types.Project) bool {
			return assert.ObjectsAreEqual([]string{"jellyfin"}, p.ServiceNames())
		}), mock.MatchedBy(func(o api.UpOptions) bool {
			return o.Start.Project != nil && assert.ObjectsAreEqual([]string{"jellyfin"}, o.Start.Project.ServiceNames()) &&
				o.Create.RemoveOrphans && o.Create.Inherit
		})).Return(nil)

		require.NoError(t, (&Runner{service: service}).Up(ctx, full, []string{"jellyfin"}, NoWait))
	})

	t.Run("up without services starts every service", func(t *testing.T) {
		full := &types.Project{Name: "media-server", Services: types.Services{
			"jellyfin": {Name: "jellyfin", Image: "j"},
			"sonarr":   {Name: "sonarr", Image: "s"},
		}}
		service := newMockService(t)
		service.EXPECT().Up(ctx, full, mock.MatchedBy(func(o api.UpOptions) bool { return o.Start.Project == full && !o.Start.Wait })).Return(nil)

		require.NoError(t, (&Runner{service: service}).Up(ctx, full, nil, NoWait))
	})

	t.Run("down removes orphans", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Down(ctx, "media-server", mock.MatchedBy(func(o api.DownOptions) bool { return o.Project == project && o.RemoveOrphans })).Return(nil)

		require.NoError(t, (&Runner{service: service}).Down(ctx, project))
	})

	t.Run("restart passes the services", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Restart(ctx, "media-server", mock.MatchedBy(func(o api.RestartOptions) bool {
			return o.Project == project && assert.ObjectsAreEqual([]string{"homepage"}, o.Services)
		})).Return(nil)

		require.NoError(t, (&Runner{service: service}).Restart(ctx, project, []string{"homepage"}))
	})

	t.Run("ps lists every container with its ports", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Ps(ctx, "media-server", api.PsOptions{Project: project, All: true}).Return([]api.ContainerSummary{
			{Name: "jellyfin", State: container.StateRunning, Health: container.Healthy, Publishers: api.PortPublishers{
				{URL: "0.0.0.0", TargetPort: 8096, PublishedPort: 8096, Protocol: "tcp"},
				{URL: "0.0.0.0", TargetPort: 1900, PublishedPort: 1900, Protocol: "udp"},
				{URL: "::", TargetPort: 8096, PublishedPort: 8096, Protocol: "tcp"},
				{TargetPort: 7359, Protocol: "udp"},
			}},
			{Name: "configarr", State: container.StateExited},
		}, nil)

		containers, err := (&Runner{service: service}).Ps(ctx, project)

		require.NoError(t, err)
		assert.Equal(t, []Container{
			{Name: "configarr", State: "exited"},
			{Name: "jellyfin", State: "running", Health: "healthy", Ports: []string{"1900->1900/udp", "8096->8096/tcp"}},
		}, containers)
	})

	t.Run("containers lists the project's containers by its name alone", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Ps(ctx, "media-server", api.PsOptions{All: true}).Return([]api.ContainerSummary{
			{Name: "sonarr", State: container.StateExited},
			{Name: "jellyfin", State: container.StateRunning},
		}, nil)

		containers, err := (&Runner{service: service}).Containers(ctx, "media-server")

		require.NoError(t, err)
		assert.Equal(t, []Container{{Name: "jellyfin", State: "running"}, {Name: "sonarr", State: "exited"}}, containers)
	})

	t.Run("logs stream as service | line", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Logs(ctx, "media-server", mock.Anything, mock.MatchedBy(func(o api.LogOptions) bool {
			return o.Project == project && o.Follow && o.Tail == "20" && assert.ObjectsAreEqual([]string{"jellyfin"}, o.Services)
		})).RunAndReturn(func(_ context.Context, _ string, consumer api.LogConsumer, _ api.LogOptions) error {
			consumer.Log("jellyfin", "started")
			consumer.Err("jellyfin", "warning")
			return nil
		})
		var out bytes.Buffer

		require.NoError(t, (&Runner{service: service}).Logs(ctx, project, LogsOptions{Follow: true, Tail: "20", Services: []string{"jellyfin"}}, &out))

		assert.Equal(t, "jellyfin | started\njellyfin | warning\n", out.String())
	})
}

func TestUpCanWaitForTheContainers(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	service := newMockService(t)
	service.EXPECT().Up(mock.Anything, project, mock.MatchedBy(func(o api.UpOptions) bool {
		return o.Start.Wait && o.Start.WaitTimeout == 2*time.Minute
	})).Return(nil)

	require.NoError(t, (&Runner{service: service}).Up(context.Background(), project, nil, Wait{Enabled: true, Timeout: 2 * time.Minute}))
}

func TestLogsColourEachService(t *testing.T) {
	paint.Enable(true)
	t.Cleanup(func() { paint.Enable(false) })
	project := &types.Project{Name: "media-server"}
	service := newMockService(t)
	service.EXPECT().Logs(mock.Anything, "media-server", mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, _ string, consumer api.LogConsumer, _ api.LogOptions) error {
		consumer.Log("sonarr", "a")
		consumer.Log("radarr", "b")
		consumer.Log("sonarr", "c")
		return nil
	})
	var out bytes.Buffer

	require.NoError(t, (&Runner{service: service}).Logs(context.Background(), project, LogsOptions{}, &out))

	cyan, yellow, reset := "\x1b[36m", "\x1b[33m", "\x1b[0m"
	assert.Equal(t, cyan+"sonarr"+reset+" | a\n"+yellow+"radarr"+reset+" | b\n"+cyan+"sonarr"+reset+" | c\n", out.String())
}

func TestStoppingAndStarting(t *testing.T) {
	ctx := context.Background()
	full := &types.Project{Name: "media-server", Services: types.Services{
		"jellyfin": {Name: "jellyfin", Image: "j"},
		"sonarr":   {Name: "sonarr", Image: "s"},
	}}

	t.Run("running services of the project, once each and sorted", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Ps(ctx, "media-server", api.PsOptions{Project: full}).Return([]api.ContainerSummary{
			{Service: "sonarr", State: container.StateRunning},
			{Service: "jellyfin", State: container.StateRunning},
			{Service: "configarr", State: container.StateExited},
			{Service: "sonarr", State: container.StateRunning},
			{Service: "homepage", State: container.StateRunning},
		}, nil)

		running, err := (&Runner{service: service}).RunningServices(ctx, full)

		require.NoError(t, err)
		assert.Equal(t, []string{"jellyfin", "sonarr"}, running)
	})

	t.Run("any running container counts, in the model or not", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Ps(ctx, "media-server", api.PsOptions{Project: full}).Return([]api.ContainerSummary{
			{Service: "configarr", State: container.StateExited},
			{Service: "homepage", State: container.StateRunning},
		}, nil)

		running, err := (&Runner{service: service}).AnyRunning(ctx, full)

		require.NoError(t, err)
		assert.True(t, running)
	})

	t.Run("stop stops the project", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Stop(ctx, "media-server", api.StopOptions{Project: full}).Return(nil)

		require.NoError(t, (&Runner{service: service}).Stop(ctx, full))
	})

	t.Run("start starts only the services given", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Start(ctx, "media-server", mock.MatchedBy(func(o api.StartOptions) bool {
			return assert.ObjectsAreEqual([]string{"sonarr"}, o.Project.ServiceNames())
		})).Return(nil)

		require.NoError(t, (&Runner{service: service}).Start(ctx, full, []string{"sonarr"}))
	})
}

func TestDetached(t *testing.T) {
	ctx := context.Background()
	project := &types.Project{Name: "media-server", Services: types.Services{
		"gluetun": {Name: "gluetun"}, "deluge": {Name: "deluge"}, "prowlarr": {Name: "prowlarr"}, "flaresolverr": {Name: "flaresolverr"},
	}}
	dependents := []string{"prowlarr", "flaresolverr", "deluge", "not-in-project"}

	type Given struct {
		containers []api.ContainerSummary
		modes      map[string]string
	}
	tests := map[string]struct {
		Given Given
		Then  []string
	}{
		"all attached": {
			Given: Given{
				containers: []api.ContainerSummary{
					{ID: "g1", Service: "gluetun", State: container.StateRunning},
					{ID: "p1", Service: "prowlarr", State: container.StateRunning},
					{ID: "f1", Service: "flaresolverr", State: container.StateRunning},
					{ID: "d1", Service: "deluge", State: container.StateRunning},
				},
				modes: map[string]string{"p1": "container:g1", "f1": "container:g1", "d1": "container:g1"},
			},
		},
		"one on an old gluetun and one missing": {
			Given: Given{
				containers: []api.ContainerSummary{
					{ID: "g2", Service: "gluetun", State: container.StateRunning},
					{ID: "p1", Service: "prowlarr", State: container.StateRunning},
					{ID: "d1", Service: "deluge", State: container.StateRunning},
				},
				modes: map[string]string{"p1": "container:g2", "d1": "container:g1"},
			},
			Then: []string{"flaresolverr", "deluge"},
		},
		"gluetun not running": {
			Given: Given{containers: []api.ContainerSummary{{ID: "g1", Service: "gluetun", State: container.StateExited}}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			service := newMockService(t)
			service.EXPECT().Ps(ctx, "media-server", api.PsOptions{Project: project, All: true}).Return(tt.Given.containers, nil)
			docker := newMockContainers(t)
			for id, mode := range tt.Given.modes {
				result := client.ContainerInspectResult{}
				result.Container.HostConfig = &container.HostConfig{NetworkMode: container.NetworkMode(mode)}
				docker.EXPECT().ContainerInspect(ctx, id, client.ContainerInspectOptions{}).Return(result, nil)
			}

			detached, err := (&Runner{service: service, docker: docker}).Detached(ctx, project, "gluetun", dependents)

			require.NoError(t, err)
			assert.Equal(t, tt.Then, detached)
		})
	}
}

func TestBindSources(t *testing.T) {
	project := &types.Project{Services: types.Services{
		"jellyfin": {Name: "jellyfin", Volumes: []types.ServiceVolumeConfig{
			{Type: types.VolumeTypeBind, Source: "/data/mse/gorgon/volumes/jellyfin"},
			{Type: types.VolumeTypeBind, Source: "/media/movies"},
			{Type: types.VolumeTypeVolume, Source: "cache"},
		}},
		"sonarr": {Name: "sonarr", Volumes: []types.ServiceVolumeConfig{
			{Type: types.VolumeTypeBind, Source: "/data/mse/gorgon/volumes/sonarr"},
			{Type: types.VolumeTypeBind, Source: "/data/mse/gorgon/volumes/jellyfin"},
		}},
	}}

	assert.Equal(t, []string{"/data/mse/gorgon/volumes/jellyfin", "/data/mse/gorgon/volumes/sonarr"}, BindSources(project, "/data/mse/gorgon"))
}

func TestPullAndRecreate(t *testing.T) {
	ctx := context.Background()

	inspected := func(id string) client.ImageInspectResult {
		result := client.ImageInspectResult{}
		result.ID = id
		return result
	}
	servicesPulled := func(names ...string) any {
		return mock.MatchedBy(func(p *types.Project) bool {
			return assert.ObjectsAreEqual(names, p.ServiceNames())
		})
	}

	t.Run("pull pulls only what isn't here at its pinned digest, and counts the images that were new", func(t *testing.T) {
		pinned := &types.Project{Name: "media-server", Services: types.Services{
			"sonarr":    {Name: "sonarr", Image: "sonarr@sha256:a"},
			"radarr":    {Name: "radarr", Image: "radarr@sha256:b"},
			"configarr": {Name: "configarr", Image: "radarr@sha256:b"},
			"built":     {Name: "built"},
			"override":  {Name: "override", Image: "busybox:latest"},
		}}
		service := newMockService(t)
		service.EXPECT().Pull(ctx, servicesPulled("configarr", "override", "radarr"), api.PullOptions{}).Return(nil)
		docker := newMockContainers(t)
		docker.EXPECT().ImageInspect(ctx, "sonarr@sha256:a").Return(inspected("s1"), nil).Twice()
		docker.EXPECT().ImageInspect(ctx, "radarr@sha256:b").Return(client.ImageInspectResult{}, cerrdefs.ErrNotFound).Once()
		docker.EXPECT().ImageInspect(ctx, "radarr@sha256:b").Return(inspected("r1"), nil).Once()
		docker.EXPECT().ImageInspect(ctx, "busybox:latest").Return(inspected("b1"), nil).Once()
		docker.EXPECT().ImageInspect(ctx, "busybox:latest").Return(inspected("b2"), nil).Once()

		pulled, err := (&Runner{service: service, docker: docker}).Pull(ctx, pinned)

		require.NoError(t, err)
		assert.Equal(t, Pulled{New: 2, Total: 3}, pulled)
	})

	t.Run("every pinned image already here asks no registry", func(t *testing.T) {
		pinned := &types.Project{Name: "media-server", Services: types.Services{
			"sonarr": {Name: "sonarr", Image: "sonarr@sha256:a"},
			"radarr": {Name: "radarr", Image: "radarr:6.4@sha256:b"},
		}}
		docker := newMockContainers(t)
		docker.EXPECT().ImageInspect(ctx, "sonarr@sha256:a").Return(inspected("s1"), nil)
		docker.EXPECT().ImageInspect(ctx, "radarr:6.4@sha256:b").Return(inspected("r1"), nil)

		pulled, err := (&Runner{service: newMockService(t), docker: docker}).Pull(ctx, pinned)

		require.NoError(t, err)
		assert.Equal(t, Pulled{Total: 2}, pulled)
	})

	t.Run("an image docker cannot inspect stops the pull", func(t *testing.T) {
		pinned := &types.Project{Name: "media-server", Services: types.Services{"sonarr": {Name: "sonarr", Image: "sonarr@sha256:a"}}}
		docker := newMockContainers(t)
		docker.EXPECT().ImageInspect(ctx, "sonarr@sha256:a").Return(client.ImageInspectResult{}, errors.New("cannot connect to the Docker daemon"))

		_, err := (&Runner{service: newMockService(t), docker: docker}).Pull(ctx, pinned)

		assert.EqualError(t, err, "cannot connect to the Docker daemon")
	})

	t.Run("recreate forces only the named services, without their dependencies", func(t *testing.T) {
		full := &types.Project{Name: "media-server", Services: types.Services{
			"gluetun": {Name: "gluetun", Image: "g"},
			"deluge":  {Name: "deluge", Image: "d", NetworkMode: "service:gluetun", DependsOn: types.DependsOnConfig{"gluetun": {Condition: "service_started"}}},
		}}
		service := newMockService(t)
		service.EXPECT().Up(ctx, mock.MatchedBy(func(p *types.Project) bool {
			return assert.ObjectsAreEqual([]string{"deluge"}, p.ServiceNames())
		}), mock.MatchedBy(func(o api.UpOptions) bool {
			return o.Create.Recreate == api.RecreateForce && o.Create.RecreateDependencies == api.RecreateNever &&
				assert.ObjectsAreEqual([]string{"deluge"}, o.Create.Services) && o.Create.Inherit
		})).Return(nil)

		require.NoError(t, (&Runner{service: service}).Recreate(ctx, full, []string{"deluge"}))
	})
}

func multiplexedLogs(text string) io.ReadCloser {
	header := make([]byte, 8)
	header[0] = 1
	binary.BigEndian.PutUint32(header[4:], uint32(len(text))) //nolint:gosec // test output is short
	return io.NopCloser(bytes.NewReader(append(header, text...)))
}

type configarrRun struct {
	service *mockService
	docker  *mockContainers
	project *types.Project
}

func newConfigarrRun(t *testing.T) configarrRun {
	t.Helper()
	project := &types.Project{Name: "media-server", Services: types.Services{
		"sonarr":    {Name: "sonarr", Image: "s"},
		"configarr": {Name: "configarr", Image: "c", DependsOn: types.DependsOnConfig{"sonarr": {Condition: "service_healthy"}}},
	}}
	run := configarrRun{service: newMockService(t), docker: newMockContainers(t), project: project}
	run.service.EXPECT().Create(mock.Anything, mock.MatchedBy(func(p *types.Project) bool {
		return assert.ObjectsAreEqual([]string{"configarr"}, p.ServiceNames()) && len(p.Services["configarr"].DependsOn) == 0
	}), mock.MatchedBy(func(o api.CreateOptions) bool {
		return assert.ObjectsAreEqual([]string{"configarr"}, o.Services) && o.Recreate == api.RecreateForce
	})).Return(nil)
	run.service.EXPECT().Ps(mock.Anything, "media-server", mock.MatchedBy(func(o api.PsOptions) bool {
		return o.All && assert.ObjectsAreEqual([]string{"configarr"}, o.Services)
	})).Return([]api.ContainerSummary{{ID: "c1", Service: "configarr"}}, nil)
	run.docker.EXPECT().ContainerStart(mock.Anything, "c1", client.ContainerStartOptions{}).Return(client.ContainerStartResult{}, nil)
	run.docker.EXPECT().ContainerLogs(mock.Anything, "c1", client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Follow: true}).Return(multiplexedLogs("INFO done\n"), nil)
	run.docker.EXPECT().ContainerRemove(mock.Anything, "c1", client.ContainerRemoveOptions{Force: true}).Return(client.ContainerRemoveResult{}, nil)
	return run
}

func TestRunOnceRunsTheServiceAloneAndRemovesItsContainer(t *testing.T) {
	run := newConfigarrRun(t)
	exited := make(chan container.WaitResponse, 1)
	exited <- container.WaitResponse{StatusCode: 3}
	run.docker.EXPECT().ContainerWait(mock.Anything, "c1", client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning}).Return(client.ContainerWaitResult{Result: exited, Error: make(chan error)})
	var out bytes.Buffer

	exit, err := (&Runner{service: run.service, docker: run.docker}).RunOnce(context.Background(), run.project, "configarr", &out)

	require.NoError(t, err)
	assert.Equal(t, 3, exit)
	assert.Equal(t, "INFO done\n", out.String())
}

func TestRunOnceStopsTheContainerWhenCancelled(t *testing.T) {
	run := newConfigarrRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	run.docker.EXPECT().ContainerWait(mock.Anything, "c1", client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning}).RunAndReturn(
		func(context.Context, string, client.ContainerWaitOptions) client.ContainerWaitResult {
			cancel()
			return client.ContainerWaitResult{Result: make(chan container.WaitResponse), Error: make(chan error)}
		})
	run.docker.EXPECT().ContainerStop(mock.MatchedBy(func(stopping context.Context) bool { return stopping.Err() == nil }), "c1", client.ContainerStopOptions{}).Return(client.ContainerStopResult{}, nil)

	_, err := (&Runner{service: run.service, docker: run.docker}).RunOnce(ctx, run.project, "configarr", io.Discard)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestDataFoldersIncludeTheDeclaredFoldersInsideTheSharedMount(t *testing.T) {
	project := &types.Project{Services: types.Services{
		"deluge": {Name: "deluge", Volumes: []types.ServiceVolumeConfig{
			{Type: types.VolumeTypeBind, Source: "/d/volumes/deluge"},
			{Type: types.VolumeTypeBind, Source: "/d/data"},
		}},
		"homepage": {Name: "homepage", Volumes: []types.ServiceVolumeConfig{
			{Type: types.VolumeTypeBind, Source: "/d/data/media"},
		}},
	}}
	declared := []string{"/data/downloads/complete", "/data/media/movies", "/data/media/anime", "/data/media/movies"}

	assert.Equal(t, []string{"/d/data", "/d/data/downloads/complete", "/d/data/media", "/d/data/media/anime", "/d/data/media/movies", "/d/volumes/deluge"}, DataFolders(project, "/d", declared))
}

func TestDataFoldersWithoutTheSharedMountAreTheBindSources(t *testing.T) {
	project := &types.Project{Services: types.Services{
		"sonarr": {Name: "sonarr", Volumes: []types.ServiceVolumeConfig{{Type: types.VolumeTypeBind, Source: "/d/volumes/sonarr"}}},
	}}

	assert.Equal(t, []string{"/d/volumes/sonarr"}, DataFolders(project, "/d", []string{"/data/media/movies"}))
}
