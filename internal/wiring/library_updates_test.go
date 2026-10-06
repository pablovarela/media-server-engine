package wiring

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	sonarrEvents = []string{"onApplicationUpdate", "onDownload", "onEpisodeFileDelete", "onEpisodeFileDeleteForUpgrade", "onGrab", "onHealthIssue", "onHealthRestored",
		"onImportComplete", "onManualInteractionRequired", "onRename", "onSeriesAdd", "onSeriesDelete", "onUpgrade"}
	radarrEvents = []string{"onApplicationUpdate", "onDownload", "onGrab", "onHealthIssue", "onHealthRestored", "onManualInteractionRequired", "onMovieAdded",
		"onMovieDelete", "onMovieFileDelete", "onMovieFileDeleteForUpgrade", "onRename", "onUpgrade"}
	leftOff = map[string]bool{"onHealthIssue": true, "onHealthRestored": true, "onManualInteractionRequired": true}
)

func notificationSchema(events []string) map[string]any {
	item := map[string]any{"name": "", "implementation": "MediaBrowser", "tags": []any{}}
	for _, event := range events {
		item[event] = false
	}
	var fields []any
	for _, f := range []struct {
		name  string
		value any
	}{{"host", nil}, {"port", 8096}, {"useSsl", false}, {"urlBase", nil}, {"apiKey", nil}, {"notify", false}, {"updateLibrary", true}, {"mapFrom", nil}, {"mapTo", nil}} {
		fields = append(fields, map[string]any{"name": f.name, "value": f.value})
	}
	item["fields"] = fields
	return item
}

func notificationConnection(kind string, events []string) map[string]any {
	item := notificationSchema(events)
	for _, event := range events {
		item[event] = !leftOff[event]
	}
	item["id"], item["name"] = 2, "Emby / Jellyfin"
	values := map[string]any{"host": "jellyfin", "apiKey": "********"}
	for _, entry := range item["fields"].([]any) {
		f := entry.(map[string]any)
		if value, ok := values[f["name"].(string)]; ok {
			f["value"] = value
		}
	}
	return item
}

func fieldsOf(body map[string]any) map[string]any {
	fields := map[string]any{}
	for _, entry := range body["fields"].([]any) {
		f := entry.(map[string]any)
		fields[f["name"].(string)] = f["value"]
	}
	return fields
}

func libraryUpdatesAnswering(t *testing.T, r *routes, sonarr, radarr []map[string]any) *routes {
	t.Helper()
	for _, arr := range []struct {
		host        string
		events      []string
		connections []map[string]any
	}{{"http://sonarr", sonarrEvents, sonarr}, {"http://radarr", radarrEvents, radarr}} {
		connections := arr.connections
		if connections == nil {
			connections = []map[string]any{}
		}
		r.on("GET", arr.host+"/api/v3/notification", ok(jsonOf(t, connections)))
		r.on("GET", arr.host+"/api/v3/notification/schema", ok(jsonOf(t, []any{notificationSchema(arr.events)})))
		r.on("POST", arr.host+"/api/v3/notification?forceSave=true")
		r.on("PUT", arr.host+"/api/v3/notification/2?forceSave=true")
	}
	return r
}

func libraryUpdatesWired(t *testing.T, r *routes, sonarr map[string]any) *routes {
	t.Helper()
	if sonarr == nil {
		sonarr = notificationConnection("sonarr", sonarrEvents)
	}
	return libraryUpdatesAnswering(t, r, []map[string]any{sonarr}, []map[string]any{notificationConnection("radarr", radarrEvents)})
}

func libraryUpdatesEnv(t *testing.T, r *routes) Env {
	t.Helper()
	env, _ := testEnv(t, r.client(), map[string]string{"SONARR_API_KEY": "sonarr-key", "RADARR_API_KEY": "radarr-key"})
	env.Settings["SONARR_URL"] = "http://sonarr"
	env.Settings["RADARR_URL"] = "http://radarr"
	storeJellyfinKey(t, env, "jellyfin-key")
	return env
}

func rememberAppliedJellyfinKey(t *testing.T, env Env, key string) {
	t.Helper()
	for _, kind := range []string{"sonarr", "radarr"} {
		require.NoError(t, env.State().Remember(kind+"-jellyfin-connection.sha256", Fingerprint(key)))
	}
}

func TestFreshSonarrAndRadarrTellJellyfinAboutEveryLibraryChange(t *testing.T) {
	r := libraryUpdatesAnswering(t, newRoutes(t), nil, nil)
	env := libraryUpdatesEnv(t, r)
	var out said
	env.Say = out.say

	require.NoError(t, LibraryUpdates(context.Background(), env))

	assert.Equal(t, []string{"POST /api/v3/notification?forceSave=true"}, r.writes("sonarr"))
	assert.Equal(t, []string{"POST /api/v3/notification?forceSave=true"}, r.writes("radarr"))
	bodies := r.sentBodies("POST", "/api/v3/notification?forceSave=true")
	sonarr, radarr := bodies[0], bodies[1]
	assert.Equal(t, "Emby / Jellyfin", sonarr["name"])
	assert.Equal(t, map[string]any{"host": "jellyfin", "port": float64(8096), "useSsl": false, "urlBase": nil, "apiKey": "jellyfin-key", "notify": false, "updateLibrary": true, "mapFrom": nil, "mapTo": nil}, fieldsOf(sonarr))
	var on, want []string
	for _, event := range sonarrEvents {
		if sonarr[event] == true {
			on = append(on, event)
		}
		if !leftOff[event] {
			want = append(want, event)
		}
	}
	sort.Strings(on)
	sort.Strings(want)
	assert.Equal(t, want, on)
	assert.Equal(t, []any{nil, nil, true}, []any{fieldsOf(radarr)["mapFrom"], fieldsOf(radarr)["mapTo"], radarr["onMovieDelete"]})
	assert.Contains(t, out.lines, "sonarr: add the Jellyfin connection")
}

func TestEachArrIsAskedWithItsOwnKey(t *testing.T) {
	r := libraryUpdatesAnswering(t, newRoutes(t), nil, nil)

	require.NoError(t, LibraryUpdates(context.Background(), libraryUpdatesEnv(t, r)))

	for _, request := range r.requests {
		assert.Equal(t, request.host+"-key", request.headers.Get("X-Api-Key"))
	}
}

func TestAlreadyConnectedArrsAreLeftUntouched(t *testing.T) {
	r := libraryUpdatesWired(t, newRoutes(t), nil)
	env := libraryUpdatesEnv(t, r)
	rememberAppliedJellyfinKey(t, env, "jellyfin-key")
	var out said
	env.Say = out.say

	require.NoError(t, LibraryUpdates(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestANewJellyfinKeyReachesBothConnectionsOnce(t *testing.T) {
	r := libraryUpdatesWired(t, newRoutes(t), nil)
	env := libraryUpdatesEnv(t, r)
	rememberAppliedJellyfinKey(t, env, "old-key")

	require.NoError(t, LibraryUpdates(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v3/notification/2?forceSave=true", "PUT /api/v3/notification/2?forceSave=true"}, r.writes())
	assert.Equal(t, "jellyfin-key", fieldsOf(r.sentBody("PUT", "/api/v3/notification/2?forceSave=true"))["apiKey"])
	r.requests = nil

	require.NoError(t, LibraryUpdates(context.Background(), env))

	assert.Empty(t, r.writes())
}

func TestAConnectionPointedElsewhereIsCorrectedKeepingItsEventsAndTheRealKey(t *testing.T) {
	moved := notificationConnection("sonarr", sonarrEvents)
	moved["onGrab"] = false
	moved["fields"].([]any)[0].(map[string]any)["value"] = "old-host"
	r := libraryUpdatesWired(t, newRoutes(t), moved)
	env := libraryUpdatesEnv(t, r)
	rememberAppliedJellyfinKey(t, env, "jellyfin-key")
	var out said
	env.Say = out.say

	require.NoError(t, LibraryUpdates(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v3/notification/2?forceSave=true"}, r.writes())
	body := r.sentBody("PUT", "/api/v3/notification/2?forceSave=true")
	assert.Equal(t, []any{"jellyfin", "jellyfin-key", false}, []any{fieldsOf(body)["host"], fieldsOf(body)["apiKey"], body["onGrab"]})
	assert.Contains(t, out.lines, "sonarr: set the Jellyfin connection host old-host -> jellyfin")
}

func TestAnOldPathMappingIsClearedSinceJellyfinSeesTheSamePaths(t *testing.T) {
	mapped := notificationConnection("sonarr", sonarrEvents)
	for _, entry := range mapped["fields"].([]any) {
		f := entry.(map[string]any)
		switch f["name"] {
		case "mapFrom":
			f["value"] = "/tv"
		case "mapTo":
			f["value"] = "/data/tvshows"
		}
	}
	r := libraryUpdatesWired(t, newRoutes(t), mapped)
	env := libraryUpdatesEnv(t, r)
	rememberAppliedJellyfinKey(t, env, "jellyfin-key")
	var out said
	env.Say = out.say

	require.NoError(t, LibraryUpdates(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v3/notification/2?forceSave=true"}, r.writes("sonarr"))
	assert.Empty(t, r.writes("radarr"))
	body := fieldsOf(r.sentBody("PUT", "/api/v3/notification/2?forceSave=true"))
	assert.Equal(t, []any{nil, nil}, []any{body["mapFrom"], body["mapTo"]})
	assert.Contains(t, out.lines, "sonarr: set the Jellyfin connection mapFrom /tv -> None")
	assert.Contains(t, out.lines, "sonarr: set the Jellyfin connection mapTo /data/tvshows -> None")
}

func TestWithoutTheStoredJellyfinKeyTheStepFailsWithAHint(t *testing.T) {
	r := newRoutes(t)
	env, _ := testEnv(t, r.client(), map[string]string{"SONARR_API_KEY": "sonarr-key", "RADARR_API_KEY": "radarr-key"})

	err := LibraryUpdates(context.Background(), env)

	assert.EqualError(t, err, "no Jellyfin key in data/volumes/.wiring/jellyfin.key; the jellyfin wiring stores it")
	assert.Empty(t, r.requests)
}

func TestAnArrThatFailsDoesNotStopTheOtherBeingConnected(t *testing.T) {
	r := libraryUpdatesAnswering(t, newRoutes(t), nil, nil)
	r.on("GET", "http://sonarr/api/v3/notification", answer{err: errors.New("connection refused by firewall")})

	err := LibraryUpdates(context.Background(), libraryUpdatesEnv(t, r))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sonarr: GET /api/v3/notification failed")
	assert.Equal(t, []string{"POST /api/v3/notification?forceSave=true"}, r.writes("radarr"))
}
