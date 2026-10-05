package compose

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
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

	t.Run("pull pulls the whole project", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Pull(ctx, project, api.PullOptions{}).Return(nil)

		require.NoError(t, (&Runner{service: service}).Pull(ctx, project))
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
			inspect := newMockInspector(t)
			for id, mode := range tt.Given.modes {
				result := client.ContainerInspectResult{}
				result.Container.HostConfig = &container.HostConfig{NetworkMode: container.NetworkMode(mode)}
				inspect.EXPECT().ContainerInspect(ctx, id, client.ContainerInspectOptions{}).Return(result, nil)
			}

			detached, err := (&Runner{service: service, inspect: inspect}).Detached(ctx, project, "gluetun", dependents)

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
