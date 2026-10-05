package compose

import (
	"bytes"
	"context"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/stretchr/testify/assert"
)

func TestEventsAreCountedAndLogged(t *testing.T) {
	type Then struct {
		outcome string
		failed  []string
	}
	tests := map[string]struct {
		Given []api.Resource
		Then  Then
	}{
		"stopping": {
			Given: []api.Resource{
				{ID: "Container seerr", Text: api.StatusStopping, Status: api.Working},
				{ID: "Container seerr", Text: api.StatusStopped, Status: api.Done},
				{ID: "Container jellyfin", Text: api.StatusStopped, Status: api.Done},
			},
			Then: Then{outcome: "stopped 2 services"},
		},
		"up with recreations and pulls": {
			Given: []api.Resource{
				{ID: "Image lscr.io/linuxserver/sonarr:4", Text: api.StatusPulled, Status: api.Done},
				{ID: "Container sonarr", Text: "Recreated", Status: api.Done},
				{ID: "Container sonarr", Text: api.StatusStarted, Status: api.Done},
				{ID: "Container radarr", Text: api.StatusStarted, Status: api.Done},
				{ID: "Container bazarr", Text: api.StatusRunning, Status: api.Done},
			},
			Then: Then{outcome: "pulled 1 image, started 2 services (1 recreated), 1 already running"},
		},
		"down": {
			Given: []api.Resource{
				{ID: "Container seerr", Text: api.StatusStopped, Status: api.Done},
				{ID: "Container seerr", Text: api.StatusRemoved, Status: api.Done},
				{ID: "Network media-server_default", Text: api.StatusRemoved, Status: api.Done},
			},
			Then: Then{outcome: "stopped 1 service, removed 1 container"},
		},
		"a failure is named": {
			Given: []api.Resource{
				{ID: "Container gluetun", Text: api.StatusError, Details: "port is already allocated", Status: api.Error},
			},
			Then: Then{outcome: "done", failed: []string{"gluetun: port is already allocated"}},
		},
		"nothing happened": {Then: Then{outcome: "done"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var tool bytes.Buffer
			outcomes := &Outcomes{}
			events := &events{tool: &tool, outcomes: outcomes}

			events.Start(context.Background(), "up")
			events.On(tt.Given...)
			events.Done("up", true)
			outcome := outcomes.Take()

			assert.Equal(t, tt.Then.outcome, outcome.String())
			assert.Equal(t, tt.Then.failed, outcome.Failed)
			for _, e := range tt.Given {
				assert.Contains(t, tool.String(), e.ID+" "+e.Text)
			}
			assert.Equal(t, "done", outcomes.Take().String(), "taking resets the count")
		})
	}
}
