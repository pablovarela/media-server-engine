package wiring

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func englishProfile() map[string]any {
	return map[string]any{
		"profileId": 1, "name": "English", "cutoff": nil, "mustContain": []any{}, "mustNotContain": []any{}, "originalFormat": 0, "tag": "english",
		"items": []any{map[string]any{"id": 1, "language": "en", "audio_exclude": "False", "hi": "False", "forced": "False", "audio_only_include": "False"}},
	}
}

func freshBazarrSettings() map[string]any {
	arr := func(port int) map[string]any {
		return map[string]any{"ip": "127.0.0.1", "base_url": "/", "ssl": false, "apikey": "", "only_monitored": false, "port": port}
	}
	return map[string]any{
		"general": map[string]any{"use_sonarr": false, "use_radarr": false, "serie_default_enabled": false, "serie_default_profile": "", "movie_default_enabled": false, "movie_default_profile": ""},
		"sonarr":  arr(8989),
		"radarr":  arr(7878),
	}
}

func wiredBazarrSettings() map[string]any {
	arr := func(ip string, port int, key string) map[string]any {
		return map[string]any{"base_url": "", "ssl": false, "only_monitored": false, "ip": ip, "port": port, "apikey": key}
	}
	return map[string]any{
		"general": map[string]any{"use_sonarr": true, "use_radarr": true, "serie_default_enabled": true, "serie_default_profile": 1, "movie_default_enabled": true, "movie_default_profile": 1},
		"sonarr":  arr("sonarr", 8989, "sonarr-key"),
		"radarr":  arr("radarr", 7878, "radarr-key"),
	}
}

func bazarrAnswering(t *testing.T, r *routes, settings map[string]any, profiles []map[string]any, enabled ...string) *routes {
	t.Helper()
	var languages []map[string]any
	for _, language := range [][2]string{{"en", "English"}, {"es", "Spanish"}, {"fr", "French"}} {
		on := false
		for _, code := range enabled {
			on = on || code == language[0]
		}
		languages = append(languages, map[string]any{"code2": language[0], "name": language[1], "enabled": on})
	}
	if profiles == nil {
		profiles = []map[string]any{}
	}
	return r.on("GET", "/api/system/settings", ok(jsonOf(t, settings))).
		on("GET", "/api/system/languages", ok(jsonOf(t, languages))).
		on("GET", "/api/system/languages/profiles", ok(jsonOf(t, profiles))).
		on("POST", "/api/system/settings")
}

func bazarrEnv(t *testing.T, r *routes, radarrKey string) (Env, *said) {
	t.Helper()
	env, out := testEnv(t, r.client(), map[string]string{"SONARR_API_KEY": "sonarr-key", "RADARR_API_KEY": radarrKey})
	env.Settings["BAZARR_URL"] = "http://bazarr"
	config := filepath.Join(env.Data, "volumes", "bazarr", "config", "config")
	require.NoError(t, os.MkdirAll(config, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(config, "config.yaml"), []byte("auth:\n  apikey: bazarr-key\n"), 0o644))
	declareLanguages(t, env, "bazarr:\n  languages: [en]\n")
	return env, out
}

func declareLanguages(t *testing.T, env Env, apps string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(env.Config, "apps.yml"), []byte(apps), 0o644))
}

type postedProfile struct {
	id        float64
	name      string
	languages []string
	tag       any
}

func profilesPosted(t *testing.T, r *routes) []postedProfile {
	t.Helper()
	var profiles []map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.sentForm("POST", "/api/system/settings").Get("languages-profiles")), &profiles))
	var posted []postedProfile
	for _, profile := range profiles {
		var languages []string
		for _, item := range profile["items"].([]any) {
			languages = append(languages, item.(map[string]any)["language"].(string))
		}
		posted = append(posted, postedProfile{profile["profileId"].(float64), profile["name"].(string), languages, profile["tag"]})
	}
	return posted
}

func TestAFreshBazarrIsConnectedToSonarrAndRadarrInOneWrite(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), freshBazarrSettings(), nil)
	env, out := bazarrEnv(t, r, "radarr-key")

	require.NoError(t, Bazarr(context.Background(), env))

	assert.Equal(t, []string{"POST /api/system/settings"}, r.writes())
	form := r.sentForm("POST", "/api/system/settings")
	assert.Equal(t, []string{"sonarr", ""}, []string{form.Get("settings-sonarr-ip"), form.Get("settings-sonarr-base_url")})
	assert.Equal(t, []string{"radarr", "radarr-key"}, []string{form.Get("settings-radarr-ip"), form.Get("settings-radarr-apikey")})
	assert.Equal(t, []string{"true", "true"}, []string{form.Get("settings-general-use_sonarr"), form.Get("settings-general-use_radarr")})
	assert.Contains(t, out.lines, "bazarr: set sonarr ip 127.0.0.1 -> sonarr")
	for _, line := range out.lines {
		assert.NotContains(t, line, "sonarr-key")
	}
}

func TestTheWiringSignsItsRequestsWithBazarrsOwnKey(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), wiredBazarrSettings(), []map[string]any{englishProfile()}, "en")
	env, _ := bazarrEnv(t, r, "radarr-key")

	require.NoError(t, Bazarr(context.Background(), env))

	for _, request := range r.requests {
		assert.Equal(t, "bazarr-key", request.headers.Get("X-API-KEY"))
	}
}

func TestAnAlreadyWiredBazarrIsLeftUntouched(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), wiredBazarrSettings(), []map[string]any{englishProfile()}, "en")
	env, out := bazarrEnv(t, r, "radarr-key")

	require.NoError(t, Bazarr(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestARotatedRadarrKeyIsSentAlone(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), wiredBazarrSettings(), []map[string]any{englishProfile()}, "en")
	env, out := bazarrEnv(t, r, "rotated")

	require.NoError(t, Bazarr(context.Background(), env))

	assert.Equal(t, "settings-radarr-apikey=rotated", r.sentForm("POST", "/api/system/settings").Encode())
	assert.Contains(t, out.lines, "bazarr: set radarr api key")
}

func TestABazarrThatHasNotWrittenItsConfigYetFailsWithAHint(t *testing.T) {
	r := newRoutes(t)
	env, _ := bazarrEnv(t, r, "radarr-key")
	require.NoError(t, os.Remove(filepath.Join(env.Data, "volumes", "bazarr", "config", "config", "config.yaml")))

	err := Bazarr(context.Background(), env)

	require.Error(t, err)
	assert.Regexp(t, `^no API key in .*config\.yaml; start bazarr once so it writes its config$`, err.Error())
	assert.Empty(t, r.requests)
}

func TestAFreshBazarrGetsTheDeclaredLanguagesInADefaultProfileForSeriesAndMovies(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), freshBazarrSettings(), nil)
	env, out := bazarrEnv(t, r, "radarr-key")

	require.NoError(t, Bazarr(context.Background(), env))

	form := r.sentForm("POST", "/api/system/settings")
	assert.Equal(t, []string{"en"}, form["languages-enabled"])
	assert.Equal(t, []postedProfile{{1, "Default", []string{"en"}, nil}}, profilesPosted(t, r))
	assert.Equal(t, []string{"true", "true"}, []string{form.Get("settings-general-serie_default_enabled"), form.Get("settings-general-movie_default_enabled")})
	assert.Equal(t, []string{"1", "1"}, []string{form.Get("settings-general-serie_default_profile"), form.Get("settings-general-movie_default_profile")})
	assert.Contains(t, out.lines, "bazarr: create subtitle profile Default with en")
}

func TestAnAddedLanguageJoinsTheDefaultProfileWhichKeepsItsNameTagAndFlags(t *testing.T) {
	hearingImpaired := englishProfile()
	hearingImpaired["items"].([]any)[0].(map[string]any)["hi"] = "True"
	r := bazarrAnswering(t, newRoutes(t), wiredBazarrSettings(), []map[string]any{hearingImpaired}, "en")
	env, out := bazarrEnv(t, r, "radarr-key")
	declareLanguages(t, env, "bazarr:\n  languages: [en, es]\n")

	require.NoError(t, Bazarr(context.Background(), env))

	form := r.sentForm("POST", "/api/system/settings")
	assert.Equal(t, []string{"en", "es"}, form["languages-enabled"])
	var profiles []map[string]any
	require.NoError(t, json.Unmarshal([]byte(form.Get("languages-profiles")), &profiles))
	require.Len(t, profiles, 1)
	assert.Equal(t, []any{"English", "english"}, []any{profiles[0]["name"], profiles[0]["tag"]})
	var items [][3]any
	for _, item := range profiles[0]["items"].([]any) {
		i := item.(map[string]any)
		items = append(items, [3]any{i["id"], i["language"], i["hi"]})
	}
	assert.Equal(t, [][3]any{{float64(1), "en", "True"}, {float64(2), "es", "False"}}, items)
	assert.NotContains(t, form, "settings-general-serie_default_profile")
	assert.Contains(t, out.lines, "bazarr: set subtitle profile English languages en -> en, es")
}

func TestOtherSubtitleProfilesAreSentBackUnchangedSinceBazarrDeletesAnyItIsNotSent(t *testing.T) {
	other := englishProfile()
	other["profileId"], other["name"], other["tag"] = 2, "Other", nil
	r := bazarrAnswering(t, newRoutes(t), wiredBazarrSettings(), []map[string]any{englishProfile(), other}, "en")
	env, _ := bazarrEnv(t, r, "radarr-key")
	declareLanguages(t, env, "bazarr:\n  languages: [en, fr]\n")

	require.NoError(t, Bazarr(context.Background(), env))

	assert.Equal(t, []postedProfile{{1, "English", []string{"en", "fr"}, "english"}, {2, "Other", []string{"en"}, nil}}, profilesPosted(t, r))
}

func TestALanguageBazarrDoesNotKnowFailsTheStepAfterSonarrAndRadarrAreConnected(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), freshBazarrSettings(), nil)
	env, _ := bazarrEnv(t, r, "radarr-key")
	declareLanguages(t, env, "bazarr:\n  languages: [en, xx]\n")

	err := Bazarr(context.Background(), env)

	assert.EqualError(t, err, "bazarr has no language xx")
	form := r.sentForm("POST", "/api/system/settings")
	assert.Equal(t, []string{"sonarr", "radarr"}, []string{form.Get("settings-sonarr-ip"), form.Get("settings-radarr-ip")})
	assert.NotContains(t, form, "languages-profiles")
}

func TestWithoutDeclaredLanguagesTheSubtitleProfilesAreLeftAlone(t *testing.T) {
	r := bazarrAnswering(t, newRoutes(t), freshBazarrSettings(), nil)
	env, _ := bazarrEnv(t, r, "radarr-key")
	declareLanguages(t, env, "")

	require.NoError(t, Bazarr(context.Background(), env))

	assert.NotContains(t, r.sentForm("POST", "/api/system/settings"), "languages-profiles")
	for _, request := range r.requests {
		assert.False(t, strings.HasPrefix(request.path, "/api/system/languages"), request.path)
	}
}

func TestADefaultProfileThatPointsElsewhereIsPointedBack(t *testing.T) {
	spanish := englishProfile()
	spanish["profileId"], spanish["name"], spanish["items"], spanish["tag"] = 7, "Spanish", []any{}, nil
	settings := wiredBazarrSettings()
	settings["general"].(map[string]any)["movie_default_profile"] = 7
	r := bazarrAnswering(t, newRoutes(t), settings, []map[string]any{englishProfile(), spanish}, "en")
	env, out := bazarrEnv(t, r, "radarr-key")

	require.NoError(t, Bazarr(context.Background(), env))

	assert.Equal(t, []string{"bazarr: set the default subtitle profile for movies Spanish -> English"}, out.lines)
	assert.Equal(t, "settings-general-movie_default_profile=1", r.sentForm("POST", "/api/system/settings").Encode())
}
