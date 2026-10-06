package wiring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	jellyfinConfiguration = `{"ServerName": "Media", "EnableMetrics": false, "UICulture": "en-US"}`
	jellyfinLibraries     = `[
		{"Name": "Collections", "Locations": [], "CollectionType": "boxsets", "ItemId": "c1", "LibraryOptions": {"EnableRealtimeMonitor": false, "Enabled": true}},
		{"Name": "Movies", "Locations": ["/data/media/movies"], "CollectionType": "movies", "ItemId": "m1", "LibraryOptions": {"EnableRealtimeMonitor": true, "Enabled": true}},
		{"Name": "Shows", "Locations": ["/data/media/tvshows"], "CollectionType": "tvshows", "ItemId": "s1", "LibraryOptions": {"EnableRealtimeMonitor": true, "Enabled": true}}]`
	storedKey = `{"AppName": "media-server", "AccessToken": "stored-key"}`
)

func jellyfinEnv(t *testing.T, r *routes) (Env, *said) {
	t.Helper()
	env, out := testEnv(t, r.client(), map[string]string{"JELLYFIN_ADMIN_PASSWORD": "admin pass"})
	env.Settings["JELLYFIN_ADMIN_USER"] = "admin"
	env.Settings["JELLYFIN_URL"] = "http://jellyfin"
	template, err := os.ReadFile(filepath.Join("..", "..", "config-template", "apps.yml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(env.Config, "apps.yml"), template, 0o644))
	return env, out
}

func storeJellyfinKey(t *testing.T, env Env, key string) {
	t.Helper()
	require.NoError(t, env.State().Remember("jellyfin.key", key))
}

func jellyfinWired(r *routes, configuration, libraries, keys string) *routes {
	if configuration == "" {
		configuration = jellyfinConfiguration
	}
	if libraries == "" {
		libraries = jellyfinLibraries
	}
	if keys == "" {
		keys = "[" + storedKey + "]"
	}
	r.on("GET", "/System/Info/Public", ok(`{"StartupWizardCompleted": true}`))
	r.on("GET", "/System/Info", ok(`{"ServerName": "Media"}`))
	r.on("GET", "/System/Configuration", ok(configuration))
	r.on("GET", "/Library/VirtualFolders", ok(libraries))
	r.on("GET", "/Auth/Keys", ok(`{"Items": `+keys+`}`))
	r.on("POST", "/Users/AuthenticateByName", ok(`{"AccessToken": "session-token"}`))
	for _, path := range []string{"/System/Configuration", "/Library/VirtualFolders/Paths?refreshLibrary=false", "/Library/VirtualFolders/LibraryOptions"} {
		r.on("POST", path)
	}
	return r
}

func TestAFreshJellyfinCompletesTheWizardThenGetsItsServerNameLibrariesAndAnAPIKey(t *testing.T) {
	r := newRoutes(t).
		on("GET", "/System/Info/Public", ok(`{"StartupWizardCompleted": false}`)).
		on("GET", "/Startup/Configuration", ok(`{"ServerName": "", "UICulture": "en-US"}`)).
		on("GET", "/Startup/User", ok(`{"Name": "abc"}`)).
		on("POST", "/Users/AuthenticateByName", ok(`{"AccessToken": "session-token"}`)).
		on("GET", "/Auth/Keys", ok(`{"Items": []}`), ok(`{"Items": [{"AppName": "media-server", "AccessToken": "new-key"}]}`)).
		on("GET", "/System/Configuration", ok(`{"ServerName": "", "EnableMetrics": false}`)).
		on("GET", "/Library/VirtualFolders", ok("[]"))
	for _, path := range []string{"/Startup/Configuration", "/Startup/User", "/Startup/RemoteAccess", "/Startup/Complete", "/Auth/Keys?app=media-server", "/System/Configuration", "/Library/Refresh"} {
		r.on("POST", path)
	}
	shows := "/Library/VirtualFolders?name=Shows&collectionType=tvshows&paths=%2Fdata%2Fmedia%2Ftvshows&refreshLibrary=false"
	movies := "/Library/VirtualFolders?name=Movies&collectionType=movies&paths=%2Fdata%2Fmedia%2Fmovies&refreshLibrary=false"
	r.on("POST", shows).on("POST", movies)
	env, out := jellyfinEnv(t, r)

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Equal(t, []string{
		"POST /Startup/Configuration", "POST /Startup/User", "POST /Startup/RemoteAccess", "POST /Startup/Complete",
		"POST /Users/AuthenticateByName", "POST /Auth/Keys?app=media-server", "POST /System/Configuration",
		"POST " + shows, "POST " + movies, "POST /Library/Refresh",
	}, r.writes())
	assert.Equal(t, "Media", r.sentBody("POST", "/Startup/Configuration")["ServerName"])
	assert.Equal(t, map[string]any{"Name": "admin", "Password": "admin pass"}, r.sentBody("POST", "/Startup/User"))
	assert.Equal(t, map[string]any{"Username": "admin", "Pw": "admin pass"}, r.sentBody("POST", "/Users/AuthenticateByName"))
	assert.Equal(t, map[string]any{"LibraryOptions": map[string]any{"EnableRealtimeMonitor": true}}, r.sentBody("POST", shows))
	assert.Equal(t, "new-key", env.State().Remembered("jellyfin.key"))
	assert.Contains(t, out.lines, "jellyfin: add library Shows")
}

func TestAnAlreadyWiredJellyfinIsLeftUntouchedIncludingLibrariesItWasNotToldAbout(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", "", "")
	env, out := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestTheJellyfinWiringSignsItsRequestsWithTheStoredKey(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", "", "")
	env, _ := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	for _, request := range r.requests {
		if request.path == "/Library/VirtualFolders" {
			assert.Contains(t, request.headers.Get("Authorization"), `Token="stored-key"`)
		}
	}
}

func TestACompletedWizardIsNotRunAgain(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", "", "")
	env, _ := jellyfinEnv(t, r)

	require.NoError(t, Jellyfin(context.Background(), env))

	for _, request := range r.requests {
		assert.False(t, strings.HasPrefix(request.path, "/Startup/"), request.path)
	}
}

func TestWithoutAStoredKeyTheOneJellyfinHasIsStoredWithoutCreatingAnother(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", "", "")
	env, _ := jellyfinEnv(t, r)

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.NotContains(t, r.writes(), "POST /Auth/Keys?app=media-server")
	assert.Equal(t, "stored-key", env.State().Remembered("jellyfin.key"))
}

func TestAStoredKeyJellyfinNoLongerAcceptsIsReplacedByTheKeyJellyfinHas(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", "", `[{"AppName": "media-server", "AccessToken": "restored-key"}]`)
	r.on("GET", "/System/Info", failing(401, ""))
	env, _ := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Contains(t, r.writes(), "POST /Users/AuthenticateByName")
	assert.Equal(t, "restored-key", env.State().Remembered("jellyfin.key"))
}

func TestADriftedServerNameIsCorrectedWithOneWriteThatKeepsTheOtherSettings(t *testing.T) {
	r := jellyfinWired(newRoutes(t), strings.Replace(jellyfinConfiguration, `"ServerName": "Media"`, `"ServerName": "Other"`, 1), "", "")
	env, out := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Equal(t, []string{"POST /System/Configuration"}, r.writes())
	assert.Equal(t, decoded(t, jellyfinConfiguration), r.sentBody("POST", "/System/Configuration"))
	assert.Contains(t, out.lines, "jellyfin: set server name Other -> Media")
}

func TestADeclaredPathMissingFromALibraryIsAddedToItAndScanned(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", strings.Replace(jellyfinLibraries, `"Locations": ["/data/media/tvshows"]`, `"Locations": []`, 1), "")
	r.on("POST", "/Library/Refresh")
	env, _ := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Equal(t, []string{"POST /Library/VirtualFolders/Paths?refreshLibrary=false", "POST /Library/Refresh"}, r.writes())
	assert.Equal(t, map[string]any{"Name": "Shows", "PathInfo": map[string]any{"Path": "/data/media/tvshows"}}, r.sentBody("POST", "/Library/VirtualFolders/Paths?refreshLibrary=false"))
}

func TestALibraryWithRealTimeMonitoringOffGetsItOnKeepingItsOtherOptions(t *testing.T) {
	off := strings.Replace(jellyfinLibraries, `"ItemId": "s1", "LibraryOptions": {"EnableRealtimeMonitor": true`, `"ItemId": "s1", "LibraryOptions": {"EnableRealtimeMonitor": false`, 1)
	r := jellyfinWired(newRoutes(t), "", off, "")
	env, out := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Equal(t, []string{"POST /Library/VirtualFolders/LibraryOptions"}, r.writes())
	assert.Equal(t, map[string]any{"Id": "s1", "LibraryOptions": map[string]any{"EnableRealtimeMonitor": true, "Enabled": true}}, r.sentBody("POST", "/Library/VirtualFolders/LibraryOptions"))
	assert.Contains(t, out.lines, "jellyfin: turn on real-time monitoring for library Shows")
}

func TestALibraryOfAnotherTypeFailsTheStepAfterTheRestIsWired(t *testing.T) {
	r := jellyfinWired(newRoutes(t),
		strings.Replace(jellyfinConfiguration, `"ServerName": "Media"`, `"ServerName": "Other"`, 1),
		strings.Replace(jellyfinLibraries, `"CollectionType": "tvshows"`, `"CollectionType": "movies"`, 1), "")
	env, _ := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	err := Jellyfin(context.Background(), env)

	assert.EqualError(t, err, "library Shows is movies, not tvshows; change it in Jellyfin")
	assert.Equal(t, []string{"POST /System/Configuration"}, r.writes())
}

func TestASignInJellyfinRefusesFailsWithTheAdminUserNamed(t *testing.T) {
	r := jellyfinWired(newRoutes(t), "", "", "")
	r.on("POST", "/Users/AuthenticateByName", failing(401, "Invalid username or password"))
	env, _ := jellyfinEnv(t, r)

	err := Jellyfin(context.Background(), env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot sign in as admin")
}

func TestWithoutADeclaredServerNameJellyfinsOwnIsLeftAlone(t *testing.T) {
	r := jellyfinWired(newRoutes(t), strings.Replace(jellyfinConfiguration, `"ServerName": "Media"`, `"ServerName": "Named by hand"`, 1), "", "")
	env, _ := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")
	require.NoError(t, os.WriteFile(filepath.Join(env.Config, "apps.yml"), []byte("jellyfin:\n  libraries: []\n"), 0o644))

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Empty(t, r.writes())
}

const (
	removeOldShows  = "/Library/VirtualFolders/Paths?name=Shows&path=%2Fdata%2Ftvshows&refreshLibrary=false"
	removeOldMovies = "/Library/VirtualFolders/Paths?name=Movies&path=%2Fdata%2Fmovies&refreshLibrary=false"
)

func TestALocationALibraryNoLongerDeclaresIsRemovedWithoutAScan(t *testing.T) {
	libraries := strings.Replace(jellyfinLibraries, `"Locations": ["/data/media/tvshows"]`, `"Locations": ["/data/tvshows", "/data/media/tvshows"]`, 1)
	r := jellyfinWired(newRoutes(t), "", libraries, "")
	r.on("DELETE", removeOldShows)
	env, out := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Equal(t, []string{"DELETE " + removeOldShows}, r.writes())
	assert.Contains(t, out.lines, "jellyfin: remove /data/tvshows from library Shows")
}

func TestAMovedLibraryGetsItsNewPathThenLosesTheOldOneAndIsScanned(t *testing.T) {
	libraries := strings.Replace(jellyfinLibraries, `"Locations": ["/data/media/movies"]`, `"Locations": ["/data/movies"]`, 1)
	r := jellyfinWired(newRoutes(t), "", libraries, "")
	r.on("DELETE", removeOldMovies)
	r.on("POST", "/Library/Refresh")
	env, out := jellyfinEnv(t, r)
	storeJellyfinKey(t, env, "stored-key")

	require.NoError(t, Jellyfin(context.Background(), env))

	assert.Equal(t, []string{"POST /Library/VirtualFolders/Paths?refreshLibrary=false", "DELETE " + removeOldMovies, "POST /Library/Refresh"}, r.writes())
	assert.Equal(t, []string{"jellyfin: add /data/media/movies to library Movies", "jellyfin: remove /data/movies from library Movies", "jellyfin: scan the libraries for what was added"}, out.lines)
}
