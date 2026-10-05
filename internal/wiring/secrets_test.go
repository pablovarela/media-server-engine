package wiring

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func without(secrets map[string]string, name string) map[string]string {
	kept := map[string]string{}
	for key, value := range secrets {
		if key != name {
			kept[key] = value
		}
	}
	return kept
}

func TestProwlarrWithoutItsKeyFailsBeforeAskingIt(t *testing.T) {
	r := newRoutes(t)
	env, _ := prowlarrEnv(t, r, prowlarrYML, without(prowlarrSecrets, "PROWLARR_API_KEY"))

	err := Prowlarr(context.Background(), env)

	assert.EqualError(t, err, "PROWLARR_API_KEY is not in the app secrets")
	assert.Empty(t, r.requests)
}

func TestTheSyncSkipsAnAppWhoseKeyIsMissing(t *testing.T) {
	_, env, _ := syncEnv(t, "["+tvIndexer+"]")
	env.Secrets = without(prowlarrSecrets, "SONARR_API_KEY")

	err := ProwlarrSync(context.Background(), env)

	assert.EqualError(t, err, "SONARR_API_KEY is not in the app secrets")
}

func TestJellyfinWithoutTheAdminPasswordIsNotSetUpWithAnEmptyOne(t *testing.T) {
	r := newRoutes(t)
	env, _ := jellyfinEnv(t, r)
	env.Secrets = map[string]string{}

	err := Jellyfin(context.Background(), env)

	assert.EqualError(t, err, "JELLYFIN_ADMIN_PASSWORD is not in the app secrets")
	assert.Empty(t, r.requests)
}

func TestJellyfinWithoutTheAdminUserIsNotSetUp(t *testing.T) {
	r := newRoutes(t)
	env, _ := jellyfinEnv(t, r)
	delete(env.Settings, "JELLYFIN_ADMIN_USER")

	err := Jellyfin(context.Background(), env)

	assert.EqualError(t, err, "JELLYFIN_ADMIN_USER is not set in installation.env")
	assert.Empty(t, r.requests)
}

func TestLibraryUpdatesWithoutAnArrsKeyStillConnectTheOther(t *testing.T) {
	r := libraryUpdatesAnswering(t, newRoutes(t), nil, nil)
	env := libraryUpdatesEnv(t, r)
	env.Secrets = without(env.Secrets, "RADARR_API_KEY")

	err := LibraryUpdates(context.Background(), env)

	assert.EqualError(t, err, "radarr: RADARR_API_KEY is not in the app secrets")
	assert.Equal(t, []string{"POST /api/v3/notification?forceSave=true"}, r.writes("sonarr"))
	assert.Empty(t, r.writes("radarr"))
}

func TestDelugeWithoutItsWebPasswordKeepsTheOneItHas(t *testing.T) {
	f := newDelugeFixture(t, "web pass")
	f.env.Secrets = map[string]string{}

	err := f.wire()

	assert.EqualError(t, err, "DELUGE_WEB_PASSWORD is not in the app secrets")
	assert.Empty(t, f.calls)
	assert.NoFileExists(t, f.config+"/web.conf")
}

func TestSeerrWithoutAnArrsKeyLeavesThatArrAlone(t *testing.T) {
	r := seerrWired(t, newRoutes(t), seerrWiredWith{})
	env, _ := seerrEnv(t, r, without(seerrSecrets, "SONARR_API_KEY"))

	err := Seerr(context.Background(), env)

	assert.EqualError(t, err, "SONARR_API_KEY is not in the app secrets")
	assert.Empty(t, r.writes())
}

func TestBazarrWithoutAnArrsKeySendsNothing(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), freshBazarrSettings(), nil)
	env, _ := bazarrEnv(t, r, "radarr-key")
	env.Secrets = without(env.Secrets, "RADARR_API_KEY")

	err := Bazarr(context.Background(), env)

	assert.EqualError(t, err, "RADARR_API_KEY is not in the app secrets")
	assert.Empty(t, r.writes())
}

func TestMaintainerrWithoutAnArrsKeyStillMakesTheOtherConnections(t *testing.T) {
	r := maintainerrFresh(newRoutes(t))
	env, _ := maintainerrEnv(t, r, "radarr-key")
	env.Secrets = without(env.Secrets, "RADARR_API_KEY")

	err := Maintainerr(context.Background(), env)

	assert.EqualError(t, err, "RADARR_API_KEY is not in the app secrets")
	assert.Equal(t, []string{"POST /api/settings/jellyfin", "POST /api/settings/seerr", "POST /api/settings/sonarr"}, r.writes())
}

func TestSecretNamesTheMissingOne(t *testing.T) {
	env, _ := testEnv(t, nil, map[string]string{"SONARR_API_KEY": "sonarr-key", "EMPTY": ""})

	key, err := env.Secret("SONARR_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sonarr-key", key)
	_, err = env.Secret("EMPTY")
	assert.EqualError(t, err, "EMPTY is not in the app secrets")
}
