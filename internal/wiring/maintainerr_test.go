package wiring

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	maintainerrOK  = `{"status": "OK", "code": 1, "message": "Success"}`
	wiredSonarrArr = `{"id": 1, "serverName": "Sonarr", "url": "http://sonarr:8989", "apiKey": "sonarr-key"}`
	wiredRadarrArr = `{"id": 1, "serverName": "Radarr", "url": "http://radarr:7878", "apiKey": "radarr-key"}`
)

func maintainerrEnv(t *testing.T, r *routes, radarrKey string) (Env, *said) {
	t.Helper()
	env, out := testEnv(t, r.client(), map[string]string{"SONARR_API_KEY": "sonarr-key", "RADARR_API_KEY": radarrKey})
	env.Settings["MAINTAINERR_URL"] = "http://maintainerr"
	seerr := filepath.Join(env.Data, "volumes", "seerr", "config")
	require.NoError(t, os.MkdirAll(seerr, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(seerr, "settings.json"), []byte(`{"main": {"apiKey": "seerr-key"}}`), 0o644))
	storeJellyfinKey(t, env, "jellyfin-key")
	return env, out
}

func maintainerrAnswering(r *routes, jellyfin, seerr, sonarr, radarr string) *routes {
	r.on("GET", "/api/settings/jellyfin", ok(jellyfin)).on("GET", "/api/settings/seerr", ok(seerr))
	r.on("GET", "/api/settings/sonarr", ok(sonarr)).on("GET", "/api/settings/radarr", ok(radarr))
	r.on("POST", "/api/settings/jellyfin", ok(maintainerrOK)).on("POST", "/api/settings/seerr", ok(maintainerrOK))
	r.on("POST", "/api/settings/sonarr").on("POST", "/api/settings/radarr")
	return r.on("PUT", "/api/settings/sonarr/1").on("PUT", "/api/settings/radarr/1")
}

func maintainerrFresh(r *routes) *routes {
	return maintainerrAnswering(r, `{"jellyfin_url": null, "jellyfin_api_key": null}`, `{"url": null, "api_key": null}`, "[]", "[]")
}

func maintainerrWired(r *routes) *routes {
	return maintainerrAnswering(r, `{"jellyfin_url": "http://jellyfin:8096", "jellyfin_api_key": "jellyfin-key", "jellyfin_user_id": "u1"}`,
		`{"url": "http://seerr:5055", "api_key": "seerr-key"}`, "["+wiredSonarrArr+"]", "["+wiredRadarrArr+"]")
}

func TestAFreshMaintainerrIsConnectedToJellyfinSeerrSonarrAndRadarr(t *testing.T) {
	r := maintainerrFresh(newRoutes(t))
	env, out := maintainerrEnv(t, r, "radarr-key")

	require.NoError(t, Maintainerr(context.Background(), env))

	assert.Equal(t, []string{"POST /api/settings/jellyfin", "POST /api/settings/seerr", "POST /api/settings/sonarr", "POST /api/settings/radarr"}, r.writes())
	assert.Equal(t, map[string]any{"jellyfin_url": "http://jellyfin:8096", "jellyfin_api_key": "jellyfin-key"}, r.sentBody("POST", "/api/settings/jellyfin"))
	assert.Equal(t, map[string]any{"url": "http://seerr:5055", "api_key": "seerr-key"}, r.sentBody("POST", "/api/settings/seerr"))
	assert.Equal(t, map[string]any{"serverName": "Sonarr", "url": "http://sonarr:8989", "apiKey": "sonarr-key"}, r.sentBody("POST", "/api/settings/sonarr"))
	assert.Contains(t, out.lines, "maintainerr: connect jellyfin")
	for _, line := range out.lines {
		assert.NotContains(t, line, "sonarr-key")
	}
}

func TestAnAlreadyWiredMaintainerrIsLeftUntouched(t *testing.T) {
	r := maintainerrWired(newRoutes(t))
	env, out := maintainerrEnv(t, r, "radarr-key")

	require.NoError(t, Maintainerr(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestARotatedRadarrKeyIsCorrectedWithOneWrite(t *testing.T) {
	r := maintainerrWired(newRoutes(t))
	env, _ := maintainerrEnv(t, r, "rotated")

	require.NoError(t, Maintainerr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/settings/radarr/1"}, r.writes())
	assert.Equal(t, map[string]any{"serverName": "Radarr", "url": "http://radarr:7878", "apiKey": "rotated"}, r.sentBody("PUT", "/api/settings/radarr/1"))
}

func TestAnArrMaintainerrKnowsUnderAnotherNameIsFoundByItsAddressAndRenamed(t *testing.T) {
	r := maintainerrWired(newRoutes(t))
	r.on("GET", "/api/settings/sonarr", ok(`[{"id": 1, "serverName": "TV", "url": "http://sonarr:8989", "apiKey": "sonarr-key"}]`))
	env, _ := maintainerrEnv(t, r, "radarr-key")

	require.NoError(t, Maintainerr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/settings/sonarr/1"}, r.writes())
	assert.Equal(t, "Sonarr", r.sentBody("PUT", "/api/settings/sonarr/1")["serverName"])
}

func TestANewJellyfinKeyIsSentAgain(t *testing.T) {
	r := maintainerrWired(newRoutes(t))
	env, _ := maintainerrEnv(t, r, "radarr-key")
	storeJellyfinKey(t, env, "newer-key")

	require.NoError(t, Maintainerr(context.Background(), env))

	assert.Equal(t, []string{"POST /api/settings/jellyfin"}, r.writes())
	assert.Equal(t, "newer-key", r.sentBody("POST", "/api/settings/jellyfin")["jellyfin_api_key"])
}

func TestWithoutTheStoredJellyfinKeyTheOtherConnectionsAreStillMade(t *testing.T) {
	r := maintainerrFresh(newRoutes(t))
	env, _ := maintainerrEnv(t, r, "radarr-key")
	require.NoError(t, os.Remove(filepath.Join(env.State().Dir, "jellyfin.key")))

	err := Maintainerr(context.Background(), env)

	assert.EqualError(t, err, "no Jellyfin key in data/volumes/.wiring/jellyfin.key; the jellyfin wiring stores it")
	assert.Equal(t, []string{"POST /api/settings/seerr", "POST /api/settings/sonarr", "POST /api/settings/radarr"}, r.writes())
}

func TestASettingMaintainerrAnswersAsNotOKFailsTheStepAfterTheOthersAreMade(t *testing.T) {
	r := maintainerrFresh(newRoutes(t))
	r.on("POST", "/api/settings/seerr", ok(`{"status": "NOK", "code": 0, "message": "Seerr did not answer"}`))
	env, _ := maintainerrEnv(t, r, "radarr-key")

	err := Maintainerr(context.Background(), env)

	assert.EqualError(t, err, "POST /api/settings/seerr: Seerr did not answer")
	assert.Contains(t, r.writes(), "POST /api/settings/radarr")
}

func TestASeerrThatHasNotWrittenItsSettingsYetFailsThatConnectionOnly(t *testing.T) {
	r := maintainerrFresh(newRoutes(t))
	env, _ := maintainerrEnv(t, r, "radarr-key")
	require.NoError(t, os.Remove(filepath.Join(env.Data, "volumes", "seerr", "config", "settings.json")))

	err := Maintainerr(context.Background(), env)

	require.Error(t, err)
	assert.Regexp(t, `^no Seerr API key in .*settings\.json$`, err.Error())
	assert.Equal(t, []string{"POST /api/settings/jellyfin", "POST /api/settings/sonarr", "POST /api/settings/radarr"}, r.writes())
}
