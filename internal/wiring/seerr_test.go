package wiring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const (
	sonarrTest = `{"profiles": [{"id": 4, "name": "Any"}, {"id": 7, "name": "WEB-1080p"}], "rootFolders": [{"id": 1, "path": "/data/media/tvshows"}]}`
	radarrTest = `{"profiles": [{"id": 7, "name": "HD Bluray + WEB"}], "rootFolders": [{"id": 1, "path": "/data/media/movies"}]}`
)

var (
	seerrLibraries = []map[string]any{
		{"id": "m1", "name": "Movies", "enabled": true, "type": "movie"},
		{"id": "s1", "name": "Shows", "enabled": true, "type": "show"},
		{"id": "c1", "name": "Collections", "enabled": false, "type": "movie"},
	}
	seerrSecrets = map[string]string{"SONARR_API_KEY": "sonarr-key", "RADARR_API_KEY": "radarr-key", "JELLYFIN_ADMIN_PASSWORD": "admin pass"}
)

func seerrSonarr(changes map[string]any) map[string]any {
	entry := map[string]any{
		"id": 0, "name": "Sonarr", "hostname": "sonarr", "port": 8989, "apiKey": "sonarr-key", "useSsl": false, "baseUrl": "",
		"activeProfileId": 7, "activeProfileName": "WEB-1080p", "activeDirectory": "/data/media/tvshows",
		"activeAnimeProfileId": 7, "activeAnimeProfileName": "WEB-1080p", "activeAnimeDirectory": "/data/media/tvshows",
		"is4k": false, "isDefault": true, "enableSeasonFolders": false, "syncEnabled": true, "preventSearch": false, "tags": []any{}, "animeTags": []any{},
	}
	for key, value := range changes {
		entry[key] = value
	}
	return entry
}

func seerrRadarr(changes map[string]any) map[string]any {
	entry := map[string]any{
		"id": 0, "name": "Radarr", "hostname": "radarr", "port": 7878, "apiKey": "radarr-key", "useSsl": false, "baseUrl": "",
		"activeProfileId": 7, "activeProfileName": "HD Bluray + WEB", "activeDirectory": "/data/media/movies",
		"is4k": false, "minimumAvailability": "released", "isDefault": true, "syncEnabled": true, "preventSearch": false, "tags": []any{},
	}
	for key, value := range changes {
		entry[key] = value
	}
	return entry
}

func seerrEnv(t *testing.T, r *routes, secrets map[string]string) (Env, *said) {
	t.Helper()
	if secrets == nil {
		secrets = seerrSecrets
	}
	env, out := testEnv(t, r.client(), secrets)
	env.Settings["JELLYFIN_ADMIN_USER"] = "admin"
	env.Settings["SEERR_URL"] = "http://seerr"
	settings := filepath.Join(env.Data, "volumes", "seerr", "config")
	require.NoError(t, os.MkdirAll(settings, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(settings, "settings.json"), []byte(`{"main": {"apiKey": "seerr-key"}}`), 0o644))
	template, err := os.ReadFile(filepath.Join("..", "..", "config-template", "apps.yml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(env.Config, "apps.yml"), template, 0o644))
	return env, out
}

func changeApps(t *testing.T, env Env, change func(apps map[string]any)) {
	t.Helper()
	path := filepath.Join(env.Config, "apps.yml")
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	apps := map[string]any{}
	require.NoError(t, yaml.Unmarshal(content, &apps))
	change(apps)
	changed, err := yaml.Marshal(apps)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, changed, 0o644))
}

func testsOfTheArrs(r *routes) *routes {
	return r.on("POST", "/api/v1/settings/sonarr/test", ok(sonarrTest)).on("POST", "/api/v1/settings/radarr/test", ok(radarrTest))
}

func seerrFresh(t *testing.T, r *routes) *routes {
	t.Helper()
	disabled := make([]map[string]any, len(seerrLibraries))
	for n, library := range seerrLibraries {
		disabled[n] = map[string]any{"id": library["id"], "name": library["name"], "enabled": false, "type": library["type"]}
	}
	r.on("GET", "/api/v1/settings/public", ok(`{"initialized": false, "mediaServerType": 4}`))
	r.on("POST", "/api/v1/auth/jellyfin", ok(`{"id": 1}`))
	r.on("GET", "/api/v1/settings/jellyfin/library", ok("[]"))
	r.on("POST", "/api/v1/settings/jellyfin/library/sync", ok(jsonOf(t, disabled)))
	r.on("PUT", "/api/v1/settings/jellyfin/library/s1").on("PUT", "/api/v1/settings/jellyfin/library/m1")
	for _, kind := range []string{"sonarr", "radarr"} {
		r.on("GET", "/api/v1/settings/"+kind, ok("[]")).on("POST", "/api/v1/settings/"+kind)
	}
	return testsOfTheArrs(r).on("POST", "/api/v1/settings/initialize", ok(`{"initialized": true}`))
}

type seerrWiredWith struct {
	sonarr, radarr, jellyfin map[string]any
	libraries                []map[string]any
}

func seerrWired(t *testing.T, r *routes, with seerrWiredWith) *routes {
	t.Helper()
	if with.sonarr == nil {
		with.sonarr = seerrSonarr(nil)
	}
	if with.radarr == nil {
		with.radarr = seerrRadarr(nil)
	}
	if with.libraries == nil {
		with.libraries = seerrLibraries
	}
	if with.jellyfin == nil {
		with.jellyfin = map[string]any{"externalHostname": ""}
	}
	r.on("GET", "/api/v1/settings/public", ok(`{"initialized": true, "mediaServerType": 2}`))
	r.on("GET", "/api/v1/settings/jellyfin/library", ok(jsonOf(t, with.libraries)))
	r.on("GET", "/api/v1/settings/jellyfin", ok(jsonOf(t, with.jellyfin)))
	r.on("GET", "/api/v1/settings/sonarr", ok(jsonOf(t, []any{with.sonarr})))
	r.on("GET", "/api/v1/settings/radarr", ok(jsonOf(t, []any{with.radarr})))
	testsOfTheArrs(r)
	for _, path := range []string{"/api/v1/settings/sonarr/0", "/api/v1/settings/radarr/0", "/api/v1/settings/jellyfin/library/s1"} {
		r.on("PUT", path)
	}
	return r.on("POST", "/api/v1/settings/jellyfin")
}

func withoutID(entry map[string]any) map[string]any {
	copied := map[string]any{}
	for key, value := range entry {
		if key != "id" {
			copied[key] = value
		}
	}
	return copied
}

func TestAFreshSeerrSignsInWithJellyfinGetsLibrariesSonarrAndRadarrThenIsInitialised(t *testing.T) {
	r := seerrFresh(t, newRoutes(t))
	env, _ := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{
		"POST /api/v1/auth/jellyfin", "POST /api/v1/settings/jellyfin/library/sync",
		"PUT /api/v1/settings/jellyfin/library/s1", "PUT /api/v1/settings/jellyfin/library/m1",
		"POST /api/v1/settings/sonarr/test", "POST /api/v1/settings/sonarr",
		"POST /api/v1/settings/radarr/test", "POST /api/v1/settings/radarr",
		"POST /api/v1/settings/initialize",
	}, r.writes())
	auth := r.sentBody("POST", "/api/v1/auth/jellyfin")
	assert.Equal(t, []any{"admin", "admin pass", "jellyfin", float64(8096), float64(2)}, []any{auth["username"], auth["password"], auth["hostname"], auth["port"], auth["serverType"]})
	sonarr := r.sentBody("POST", "/api/v1/settings/sonarr")
	assert.Equal(t, []any{float64(7), "/data/media/tvshows", "sonarr-key", true}, []any{sonarr["activeProfileId"], sonarr["activeDirectory"], sonarr["apiKey"], sonarr["isDefault"]})
	assert.Equal(t, []any{float64(7), "/data/media/tvshows", false}, []any{sonarr["activeAnimeProfileId"], sonarr["activeAnimeDirectory"], sonarr["enableSeasonFolders"]})
	radarr := r.sentBody("POST", "/api/v1/settings/radarr")
	assert.Equal(t, []any{float64(7), "/data/media/movies", "released"}, []any{radarr["activeProfileId"], radarr["activeDirectory"], radarr["minimumAvailability"]})
	assert.Equal(t, map[string]any{"enabled": true}, r.sentBody("PUT", "/api/v1/settings/jellyfin/library/s1"))
}

func TestTheWiringSignsItsRequestsWithSeerrsOwnKey(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{})
	env, _ := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	for _, request := range r.requests {
		assert.Equal(t, "seerr-key", request.headers.Get("X-Api-Key"))
	}
}

func TestAnAlreadyWiredSeerrIsLeftUntouched(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{})
	env, out := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestARotatedSonarrKeyIsCorrectedWithOneWriteThatKeepsTheOtherSettings(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{})
	env, out := seerrEnv(t, r, map[string]string{"SONARR_API_KEY": "rotated", "RADARR_API_KEY": "radarr-key", "JELLYFIN_ADMIN_PASSWORD": "admin pass"})

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v1/settings/sonarr/0"}, r.writes())
	assert.Equal(t, decoded(t, jsonOf(t, withoutID(seerrSonarr(map[string]any{"apiKey": "rotated"})))), r.sentBody("PUT", "/api/v1/settings/sonarr/0"))
	assert.Contains(t, out.lines, "seerr: set sonarr api key")
}

func TestAChangedQualityProfileIsLookedUpByName(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{sonarr: seerrSonarr(map[string]any{"activeProfileId": 4, "activeProfileName": "Any"})})
	env, out := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{"POST /api/v1/settings/sonarr/test", "PUT /api/v1/settings/sonarr/0"}, r.writes())
	body := r.sentBody("PUT", "/api/v1/settings/sonarr/0")
	assert.Equal(t, []any{float64(7), "WEB-1080p", float64(7)}, []any{body["activeProfileId"], body["activeProfileName"], body["activeAnimeProfileId"]})
	assert.Contains(t, out.lines, "seerr: set sonarr quality profile Any -> WEB-1080p")
}

func TestAChangedMinimumAvailabilityForRadarrIsCorrected(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{radarr: seerrRadarr(map[string]any{"minimumAvailability": "announced"})})
	env, out := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v1/settings/radarr/0"}, r.writes())
	assert.Equal(t, "released", r.sentBody("PUT", "/api/v1/settings/radarr/0")["minimumAvailability"])
	assert.Contains(t, out.lines, "seerr: set radarr minimum availability announced -> released")
}

func TestADeclaredLibraryThatIsNotEnabledIsEnabledWithoutASync(t *testing.T) {
	libraries := []map[string]any{seerrLibraries[0], {"id": "s1", "name": "Shows", "enabled": false, "type": "show"}, seerrLibraries[2]}
	r := seerrWired(t, newRoutes(t), seerrWiredWith{libraries: libraries})
	env, _ := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v1/settings/jellyfin/library/s1"}, r.writes())
}

func TestAQualityProfileAnAppDoesNotHaveFailsTheStepAfterTheOtherAppIsWiredAndLeavesSeerrToFinishNextTime(t *testing.T) {
	r := seerrFresh(t, newRoutes(t))
	env, _ := seerrEnv(t, r, nil)
	changeApps(t, env, func(apps map[string]any) {
		apps["seerr"].(map[string]any)["sonarr"].(map[string]any)["quality_profile"] = "Missing"
	})

	err := Seerr(context.Background(), env)

	assert.EqualError(t, err, "sonarr has no quality profile Missing")
	assert.Contains(t, r.writes(), "POST /api/v1/settings/radarr")
	assert.NotContains(t, r.writes(), "POST /api/v1/settings/sonarr")
	assert.NotContains(t, r.writes(), "POST /api/v1/settings/initialize")
}

func TestADeclaredLibraryJellyfinDoesNotHaveFailsTheStep(t *testing.T) {
	r := seerrFresh(t, newRoutes(t))
	env, _ := seerrEnv(t, r, nil)
	changeApps(t, env, func(apps map[string]any) { apps["seerr"].(map[string]any)["libraries"] = []any{"Shows", "Cartoons"} })

	err := Seerr(context.Background(), env)

	assert.EqualError(t, err, "jellyfin has no library Cartoons")
}

func TestASeerrThatHasNotWrittenItsSettingsYetFailsWithAHint(t *testing.T) {
	r := newRoutes(t)
	env, _ := seerrEnv(t, r, nil)
	require.NoError(t, os.Remove(filepath.Join(env.Data, "volumes", "seerr", "config", "settings.json")))

	err := Seerr(context.Background(), env)

	require.Error(t, err)
	assert.Regexp(t, `^no API key in .*settings\.json; start seerr once so it writes its settings$`, err.Error())
	assert.Empty(t, r.requests)
}

func TestADeclaredJellyfinExternalURLIsSetWithOneWriteOfThatSettingAlone(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{jellyfin: map[string]any{"name": "Media", "externalHostname": "http://media.local:8096"}})
	env, out := seerrEnv(t, r, nil)
	changeApps(t, env, func(apps map[string]any) {
		apps["seerr"].(map[string]any)["jellyfin_external_url"] = "http://gorgon.local:8096"
	})

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{"POST /api/v1/settings/jellyfin"}, r.writes())
	assert.Equal(t, map[string]any{"externalHostname": "http://gorgon.local:8096"}, r.sentBody("POST", "/api/v1/settings/jellyfin"))
	assert.Contains(t, out.lines, "seerr: set jellyfin external url http://media.local:8096 -> http://gorgon.local:8096")
}

func TestAnExternalURLSeerrAlreadyHasIsNotWrittenAgain(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{jellyfin: map[string]any{"externalHostname": "http://gorgon.local:8096"}})
	env, _ := seerrEnv(t, r, nil)
	changeApps(t, env, func(apps map[string]any) {
		apps["seerr"].(map[string]any)["jellyfin_external_url"] = "http://gorgon.local:8096"
	})

	require.NoError(t, Seerr(context.Background(), env))

	assert.Empty(t, r.writes())
}

func TestARootFolderAnAppDoesNotHaveFailsTheStepWithTheFolderNamed(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{})
	env, _ := seerrEnv(t, r, nil)
	changeApps(t, env, func(apps map[string]any) {
		apps["seerr"].(map[string]any)["radarr"].(map[string]any)["root_folder"] = "/films"
	})

	err := Seerr(context.Background(), env)

	assert.EqualError(t, err, "radarr has no root folder /films")
}

func TestAPortChangedByHandInSeerrIsPutBack(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{sonarr: seerrSonarr(map[string]any{"port": 9999})})
	env, out := seerrEnv(t, r, nil)

	require.NoError(t, Seerr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v1/settings/sonarr/0"}, r.writes())
	assert.Equal(t, float64(8989), r.sentBody("PUT", "/api/v1/settings/sonarr/0")["port"])
	assert.Contains(t, out.lines, "seerr: set sonarr port 9999 -> 8989")
}

func TestAChangedRootFolderIsAppliedToSeriesAndAnime(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{})
	r.on("POST", "/api/v1/settings/sonarr/test", ok(strings.Replace(sonarrTest, `[{"id": 1, "path": "/data/media/tvshows"}]`, `[{"id": 1, "path": "/data/media/tvshows"}, {"id": 2, "path": "/series"}]`, 1)))
	env, out := seerrEnv(t, r, nil)
	changeApps(t, env, func(apps map[string]any) {
		apps["seerr"].(map[string]any)["sonarr"].(map[string]any)["root_folder"] = "/series"
	})

	require.NoError(t, Seerr(context.Background(), env))

	body := r.sentBody("PUT", "/api/v1/settings/sonarr/0")
	assert.Equal(t, []any{"/series", "/series"}, []any{body["activeDirectory"], body["activeAnimeDirectory"]})
	assert.Contains(t, out.lines, "seerr: set sonarr root folder /data/media/tvshows -> /series")
}
