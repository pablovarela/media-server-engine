package wiring

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheEngineWiresTheAppsInDependencyOrder(t *testing.T) {
	var names []string
	for _, step := range Steps(nil, nil, io.Discard) {
		names = append(names, step.Name)
	}

	assert.Equal(t, []string{"prowlarr", "jellyfin", "library-updates", "deluge", "configarr", "seerr", "bazarr", "maintainerr", "prowlarr-sync"}, names)
}

func TestEachStepWaitsForTheAppsItChanges(t *testing.T) {
	waits := map[string][]string{}
	for _, step := range Steps(nil, nil, io.Discard) {
		waits[step.Name] = step.Apps
		for _, app := range step.Apps {
			assert.Contains(t, HealthChecks, app, step.Name)
		}
	}

	assert.Equal(t, []string{"sonarr", "radarr"}, waits["library-updates"])
	assert.Equal(t, []string{"prowlarr", "sonarr", "radarr"}, waits["prowlarr-sync"])
	assert.Empty(t, waits["configarr"])
}

func TestJellyfinIsWiredOnceItsAPIAnswersWhichItDoesAfterItsHealthCheck(t *testing.T) {
	assert.Equal(t, Health{Variable: "JELLYFIN_URL", Fallback: "http://localhost:8096", Path: "/System/Info/Public"}, HealthChecks["jellyfin"])
}
