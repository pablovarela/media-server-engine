package wiring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const bazarrApp = "bazarr"

type bazarrArr struct {
	kind, ip string
	port     int
	key      string
}

var bazarrArrs = []bazarrArr{
	{sonarrKind, sonarrKind, 8989, sonarrKeyName},
	{radarrKind, radarrKind, 7878, radarrKeyName},
}

type bazarrForm struct {
	values url.Values
}

func (f bazarrForm) set(key string, values ...string) { f.values[key] = values }

func bazarrConfig(env Env) string {
	return filepath.Join(env.Data, "volumes", "bazarr", "config", "config", "config.yaml")
}

func bazarrKey(env Env) (string, error) {
	content, err := os.ReadFile(bazarrConfig(env)) //nolint:gosec // Bazarr's own config
	var config struct {
		Auth struct {
			APIKey string `yaml:"apikey"`
		} `yaml:"auth"`
	}
	if err != nil || yaml.Unmarshal(content, &config) != nil || config.Auth.APIKey == "" {
		return "", Error{Message: fmt.Sprintf("no API key in %s; start bazarr once so it writes its config", bazarrConfig(env))}
	}
	return config.Auth.APIKey, nil
}

func Bazarr(ctx context.Context, env Env) error {
	key, err := bazarrKey(env)
	if err != nil {
		return err
	}
	api := &API{Env: env, Base: env.URL("BAZARR_URL", "http://localhost:6767"), Headers: map[string]string{"X-API-KEY": key}}
	var settings map[string]any
	if err := api.Get(ctx, "/api/system/settings", &settings); err != nil {
		return err
	}
	form := bazarrForm{values: url.Values{}}
	connectArrs(env, section(settings["general"]), settings, form)
	declared, err := env.Declared("apps.yml")
	if err != nil {
		return err
	}
	var languageFailure error
	if languages := list(section(declared[bazarrApp])["languages"]); len(languages) > 0 {
		codes := make([]string, len(languages))
		for n, code := range languages {
			codes[n] = text(code)
		}
		if languageFailure = wireLanguages(ctx, env, api, section(settings["general"]), codes, form); languageFailure != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if len(form.values) > 0 {
		if err := api.SendForm(ctx, "/api/system/settings", form.values, nil); err != nil {
			return err
		}
	}
	return languageFailure
}

func connectArrs(env Env, general, settings map[string]any, form bazarrForm) {
	for _, arr := range bazarrArrs {
		current := section(settings[arr.kind])
		for _, f := range []setting{{"ip", arr.ip}, {portField, arr.port}, {"base_url", ""}} {
			if show(current[f.name]) != show(f.value) {
				env.Change(bazarrApp, fmt.Sprintf("set %s %s %s -> %s", arr.kind, f.name, show(current[f.name]), show(f.value)))
				form.set(fmt.Sprintf("settings-%s-%s", arr.kind, f.name), show(f.value))
			}
		}
		if key := env.Secrets[arr.key]; current["apikey"] != key {
			env.Change(bazarrApp, fmt.Sprintf("set %s api key", arr.kind))
			form.set(fmt.Sprintf("settings-%s-apikey", arr.kind), key)
		}
		if general["use_"+arr.kind] != true {
			env.Change(bazarrApp, "turn on use_"+arr.kind)
			form.set("settings-general-use_"+arr.kind, "true")
		}
	}
}

func wireLanguages(ctx context.Context, env Env, api *API, general map[string]any, languages []string, form bazarrForm) error {
	var known []map[string]any
	if err := api.Get(ctx, "/api/system/languages", &known); err != nil {
		return err
	}
	if err := enableLanguages(env, known, languages, form); err != nil {
		return err
	}
	var profiles []map[string]any
	if err := api.Get(ctx, "/api/system/languages/profiles", &profiles); err != nil {
		return err
	}
	profile, err := wireProfile(env, profiles, general, languages, form)
	if err != nil {
		return err
	}
	pointDefaultsAt(env, profiles, general, profile, form)
	return nil
}

func enableLanguages(env Env, known []map[string]any, languages []string, form bazarrForm) error {
	codes, enabled := map[string]bool{}, []string{}
	for _, language := range known {
		codes[text(language["code2"])] = true
		if language["enabled"] == true {
			enabled = append(enabled, text(language["code2"]))
		}
	}
	var unknown, missing []string
	for _, code := range languages {
		if !codes[code] {
			unknown = append(unknown, code)
		}
		if !containsString(enabled, code) {
			missing = append(missing, code)
		}
	}
	if len(unknown) > 0 {
		return Error{Message: "bazarr has no language " + strings.Join(unknown, ", ")}
	}
	if len(missing) > 0 {
		env.Change(bazarrApp, "enable languages "+strings.Join(missing, ", "))
		form.set("languages-enabled", append(enabled, missing...)...)
	}
	return nil
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func wireProfile(env Env, profiles []map[string]any, general map[string]any, languages []string, form bazarrForm) (map[string]any, error) {
	profile := defaultProfile(profiles, general)
	if profile == nil {
		highest := 0
		for _, p := range profiles {
			if id, ok := p["profileId"].(float64); ok && int(id) > highest {
				highest = int(id)
			}
		}
		profile = map[string]any{
			"profileId": highest + 1, "name": "Default", "cutoff": nil, "items": profileItems(languages, nil),
			"mustContain": []any{}, "mustNotContain": []any{}, "originalFormat": 0, "tag": nil,
		}
		env.Change(bazarrApp, "create subtitle profile Default with "+strings.Join(languages, ", "))
		return profile, setProfiles(form, append(profiles, profile))
	}
	var current []string
	for _, item := range entries(profile["items"]) {
		current = append(current, text(item["language"]))
	}
	if strings.Join(current, ",") == strings.Join(languages, ",") {
		return profile, nil
	}
	env.Change(bazarrApp, fmt.Sprintf("set subtitle profile %s languages %s -> %s", text(profile[nameKey]), strings.Join(current, ", "), strings.Join(languages, ", ")))
	profile["items"] = profileItems(languages, entries(profile["items"]))
	return profile, setProfiles(form, profiles)
}

func setProfiles(form bazarrForm, profiles []map[string]any) error {
	encoded, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	form.set("languages-profiles", string(encoded))
	return nil
}

func profileItems(languages []string, current []map[string]any) []any {
	flags := map[string]map[string]any{}
	for _, item := range current {
		flags[text(item["language"])] = item
	}
	items := make([]any, len(languages))
	for n, code := range languages {
		item := map[string]any{"audio_exclude": pythonFalse, "hi": pythonFalse, "forced": pythonFalse, "audio_only_include": pythonFalse}
		for key, value := range flags[code] {
			item[key] = value
		}
		item["id"], item["language"] = n+1, code
		items[n] = item
	}
	return items
}

func defaultProfile(profiles []map[string]any, general map[string]any) map[string]any {
	if general["serie_default_enabled"] != true {
		return nil
	}
	for _, profile := range profiles {
		if show(profile["profileId"]) == show(general["serie_default_profile"]) {
			return profile
		}
	}
	return nil
}

func pointDefaultsAt(env Env, profiles []map[string]any, general, profile map[string]any, form bazarrForm) {
	for _, kind := range [][2]string{{"serie", "series"}, {"movie", "movies"}} {
		enabled := general[kind[0]+"_default_enabled"] == true
		if !enabled {
			env.Change(bazarrApp, "use a default subtitle profile for "+kind[1])
			form.set("settings-general-"+kind[0]+"_default_enabled", "true")
		}
		current := general[kind[0]+"_default_profile"]
		if show(current) == show(profile["profileId"]) {
			continue
		}
		if enabled {
			name := show(current)
			for _, p := range profiles {
				if show(p["profileId"]) == show(current) {
					name = text(p[nameKey])
				}
			}
			env.Change(bazarrApp, fmt.Sprintf("set the default subtitle profile for %s %s -> %s", kind[1], name, text(profile[nameKey])))
		}
		form.set("settings-general-"+kind[0]+"_default_profile", show(profile["profileId"]))
	}
}
