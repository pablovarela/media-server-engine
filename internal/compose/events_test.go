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
		"a failure without details is named by its text": {
			Given: []api.Resource{
				{ID: "Container seerr", Text: "error while creating the container", Status: api.Error},
			},
			Then: Then{outcome: "done", failed: []string{"seerr: error while creating the container"}},
		},
		"a restart is counted as a restart": {
			Given: []api.Resource{
				{ID: "Container homepage", Text: api.StatusRestarting, Status: api.Working},
				{ID: "Container homepage", Text: api.StatusStarted, Status: api.Done},
			},
			Then: Then{outcome: "restarted 1 service"},
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

func TestPullProgressStaysOutOfTheLog(t *testing.T) {
	var tool bytes.Buffer
	events := &events{tool: &tool, outcomes: &Outcomes{}}

	events.On(
		api.Resource{ID: "Image lscr.io/linuxserver/sonarr:4", Text: api.StatusPulling, Status: api.Working},
		api.Resource{ID: "3f2c1a", ParentID: "Image lscr.io/linuxserver/sonarr:4", Text: api.StatusDownloading, Status: api.Working, Percent: 40},
		api.Resource{ID: "Image lscr.io/linuxserver/sonarr:4", Text: api.StatusPulled, Status: api.Done},
	)

	assert.Equal(t, "Image lscr.io/linuxserver/sonarr:4 Pulling\nImage lscr.io/linuxserver/sonarr:4 Pulled\n", tool.String())
}
