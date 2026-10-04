package compose

import (
	"bytes"
	"context"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestOperations(t *testing.T) {
	project := &types.Project{Name: "media-server"}
	ctx := context.Background()

	t.Run("up starts the named services and waits for nothing", func(t *testing.T) {
		service := newMockService(t)
		service.EXPECT().Up(ctx, project, mock.MatchedBy(func(o api.UpOptions) bool {
			return assert.ObjectsAreEqual([]string{"jellyfin"}, o.Create.Services) && assert.ObjectsAreEqual([]string{"jellyfin"}, o.Start.Services) && o.Start.Project == project && o.Create.RemoveOrphans
		})).Return(nil)

		require.NoError(t, (&Runner{service: service}).Up(ctx, project, []string{"jellyfin"}))
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
				{URL: "::", TargetPort: 8096, PublishedPort: 8096, Protocol: "tcp"},
				{TargetPort: 7359, Protocol: "udp"},
			}},
			{Name: "configarr", State: container.StateExited},
		}, nil)

		containers, err := (&Runner{service: service}).Ps(ctx, project)

		require.NoError(t, err)
		assert.Equal(t, []Container{
			{Name: "configarr", State: "exited"},
			{Name: "jellyfin", State: "running", Health: "healthy", Ports: []string{"8096->8096/tcp"}},
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
