package wiring

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	seerrApp          = "seerr"
	seerrUnconfigured = 4
	seerrJellyfin     = 2
	sonarrKind        = "sonarr"
	radarrKind        = "radarr"
)

type seerrArr struct {
	kind, name, hostname string
	port                 int
	key                  string
}

var seerrArrs = []seerrArr{
	{sonarrKind, "Sonarr", sonarrKind, 8989, sonarrKeyName},
	{radarrKind, "Radarr", radarrKind, 7878, radarrKeyName},
}

type seerr struct {
	env      Env
	api      *API
	failures []string
}

func seerrSettings(env Env) string {
	return filepath.Join(env.Data, "volumes", "seerr", "config", "settings.json")
}

func readSeerrKey(env Env) (string, bool) {
	content, err := os.ReadFile(seerrSettings(env)) //nolint:gosec // Seerr's own settings
	if err != nil {
		return "", false
	}
	var settings struct {
		Main struct {
			APIKey string `json:"apiKey"`
		} `json:"main"`
	}
	if json.Unmarshal(content, &settings) != nil || settings.Main.APIKey == "" {
		return "", false
	}
	return settings.Main.APIKey, true
}

func Seerr(ctx context.Context, env Env) error {
	key, found := readSeerrKey(env)
	if !found {
		return Error{Message: fmt.Sprintf("no API key in %s; start seerr once so it writes its settings", seerrSettings(env))}
	}
	declared, err := env.Declared("apps.yml")
	if err != nil {
		return err
	}
	config := section(declared[seerrApp])
	s := &seerr{env: env, api: &API{Env: env, Base: env.URL("SEERR_URL", "http://localhost:5055"), Headers: map[string]string{apiKeyHeader: key}}}
	if err := s.signInIfNeeded(ctx); err != nil {
		return err
	}
	s.attempt(ctx, func() error { return s.wireLibraries(ctx, list(config["libraries"])) })
	if url := text(config["jellyfin_external_url"]); url != "" {
		s.attempt(ctx, func() error { return s.wireExternalURL(ctx, url) })
	}
	for _, arr := range seerrArrs {
		if declaredArr, ok := config[arr.kind]; ok {
			s.attempt(ctx, func() error { return s.wireArr(ctx, arr, section(declaredArr)) })
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(s.failures) > 0 {
		return Error{Message: strings.Join(s.failures, "; ")}
	}
	return s.initialise(ctx)
}

func (s *seerr) attempt(ctx context.Context, step func() error) {
	if ctx.Err() != nil {
		return
	}
	if err := step(); err != nil {
		s.failures = append(s.failures, err.Error())
	}
}

func (s *seerr) public(ctx context.Context) (map[string]any, error) {
	var public map[string]any
	return public, s.api.Get(ctx, "/api/v1/settings/public", &public)
}

func (s *seerr) signInIfNeeded(ctx context.Context) error {
	public, err := s.public(ctx)
	if err != nil || !same(public["mediaServerType"], seerrUnconfigured) {
		return err
	}
	user, err := s.env.Setting("JELLYFIN_ADMIN_USER")
	if err != nil {
		return err
	}
	password, err := s.env.Secret("JELLYFIN_ADMIN_PASSWORD")
	if err != nil {
		return err
	}
	s.env.Change(seerrApp, "sign in with jellyfin as "+user)
	return s.api.Send(ctx, "POST", "/api/v1/auth/jellyfin", map[string]any{
		"username": user, "password": password, hostnameField: "jellyfin", portField: 8096,
		useSSLField: false, "urlBase": "", "serverType": seerrJellyfin,
	}, nil)
}

func (s *seerr) wireLibraries(ctx context.Context, names []any) error {
	var libraries []map[string]any
	if err := s.api.Get(ctx, "/api/v1/settings/jellyfin/library", &libraries); err != nil {
		return err
	}
	for _, name := range names {
		if byName(libraries, text(name)) == nil {
			s.env.Change(seerrApp, "sync libraries from jellyfin")
			libraries = nil
			if err := s.api.Send(ctx, "POST", "/api/v1/settings/jellyfin/library/sync", nil, &libraries); err != nil {
				return err
			}
			break
		}
	}
	var failures []string
	for _, name := range names {
		library := byName(libraries, text(name))
		if library == nil {
			failures = append(failures, "jellyfin has no library "+text(name))
			continue
		}
		if library["enabled"] != true {
			s.env.Change(seerrApp, "enable library "+text(name))
			if err := s.api.Send(ctx, "PUT", "/api/v1/settings/jellyfin/library/"+text(library["id"]), map[string]any{"enabled": true}, nil); err != nil {
				return err
			}
		}
	}
	if len(failures) > 0 {
		return Error{Message: strings.Join(failures, "; ")}
	}
	return nil
}

func (s *seerr) wireExternalURL(ctx context.Context, url string) error {
	var settings map[string]any
	if err := s.api.Get(ctx, "/api/v1/settings/jellyfin", &settings); err != nil {
		return err
	}
	current := text(settings["externalHostname"])
	if current == url {
		return nil
	}
	if current == "" {
		current = "(none)"
	}
	s.env.Change(seerrApp, fmt.Sprintf("set jellyfin external url %s -> %s", current, url))
	return s.api.Send(ctx, "POST", "/api/v1/settings/jellyfin", map[string]any{"externalHostname": url}, nil)
}

func (s *seerr) profileAndFolder(ctx context.Context, arr seerrArr, key string, declared map[string]any) (map[string]any, error) {
	var found map[string]any
	if err := s.api.Send(ctx, "POST", "/api/v1/settings/"+arr.kind+"/test", map[string]any{
		hostnameField: arr.hostname, portField: arr.port, apiKeyField: key, useSSLField: false, baseURLField: "",
	}, &found); err != nil {
		return nil, err
	}
	profile := byName(entries(found["profiles"]), text(declared["quality_profile"]))
	if profile == nil {
		return nil, Error{Message: fmt.Sprintf("%s has no quality profile %s", arr.kind, text(declared["quality_profile"]))}
	}
	for _, folder := range entries(found["rootFolders"]) {
		if folder["path"] == declared["root_folder"] {
			return profile, nil
		}
	}
	return nil, Error{Message: fmt.Sprintf("%s has no root folder %s", arr.kind, text(declared["root_folder"]))}
}

func setProfileAndFolder(entry map[string]any, arr seerrArr, profile map[string]any, folder any) {
	entry["activeProfileId"], entry["activeProfileName"], entry["activeDirectory"] = profile["id"], profile[nameKey], folder
	if arr.kind == sonarrKind {
		entry["activeAnimeProfileId"], entry["activeAnimeProfileName"], entry["activeAnimeDirectory"] = profile["id"], profile[nameKey], folder
	}
}

func (s *seerr) wireArr(ctx context.Context, arr seerrArr, declared map[string]any) error {
	key, err := s.env.Secret(arr.key)
	if err != nil {
		return err
	}
	var listed []map[string]any
	if err := s.api.Get(ctx, "/api/v1/settings/"+arr.kind, &listed); err != nil {
		return err
	}
	entry := defaultEntry(listed)
	if entry == nil {
		return s.addArr(ctx, arr, key, declared)
	}
	dirty := s.arrDrifted(arr, entry, key, declared)
	profileDiffers := text(entry["activeProfileName"]) != text(declared["quality_profile"])
	folderDiffers := text(entry["activeDirectory"]) != text(declared["root_folder"])
	if profileDiffers {
		s.env.Change(seerrApp, fmt.Sprintf("set %s quality profile %s -> %s", arr.kind, text(entry["activeProfileName"]), text(declared["quality_profile"])))
	}
	if folderDiffers {
		s.env.Change(seerrApp, fmt.Sprintf("set %s root folder %s -> %s", arr.kind, text(entry["activeDirectory"]), text(declared["root_folder"])))
	}
	if profileDiffers || folderDiffers {
		profile, err := s.profileAndFolder(ctx, arr, key, declared)
		if err != nil {
			return err
		}
		setProfileAndFolder(entry, arr, profile, declared["root_folder"])
		dirty = true
	}
	if !dirty {
		return nil
	}
	id := entry["id"]
	delete(entry, "id")
	return s.api.Send(ctx, "PUT", "/api/v1/settings/"+arr.kind+"/"+show(id), entry, nil)
}

func defaultEntry(listed []map[string]any) map[string]any {
	for _, entry := range listed {
		if entry["isDefault"] == true && entry["is4k"] != true {
			return entry
		}
	}
	if len(listed) > 0 {
		return listed[0]
	}
	return nil
}

func (s *seerr) arrDrifted(arr seerrArr, entry map[string]any, key string, declared map[string]any) bool {
	dirty := false
	for _, f := range []setting{{hostnameField, arr.hostname}, {portField, arr.port}} {
		if !same(entry[f.name], f.value) {
			s.env.Change(seerrApp, fmt.Sprintf("set %s %s %s -> %s", arr.kind, f.name, show(entry[f.name]), show(f.value)))
			entry[f.name] = f.value
			dirty = true
		}
	}
	if entry[apiKeyField] != key {
		s.env.Change(seerrApp, fmt.Sprintf("set %s api key", arr.kind))
		entry[apiKeyField] = key
		dirty = true
	}
	if wanted, ok := declared["minimum_availability"]; ok && arr.kind != sonarrKind && !same(entry["minimumAvailability"], wanted) {
		s.env.Change(seerrApp, fmt.Sprintf("set radarr minimum availability %s -> %s", text(entry["minimumAvailability"]), text(wanted)))
		entry["minimumAvailability"] = wanted
		dirty = true
	}
	return dirty
}

func (s *seerr) addArr(ctx context.Context, arr seerrArr, key string, declared map[string]any) error {
	s.env.Change(seerrApp, fmt.Sprintf("add %s with quality profile %s and root folder %s", arr.kind, text(declared["quality_profile"]), text(declared["root_folder"])))
	entry := map[string]any{
		"name": arr.name, hostnameField: arr.hostname, portField: arr.port, apiKeyField: key, useSSLField: false, baseURLField: "",
		"is4k": false, "isDefault": true, "syncEnabled": true, "preventSearch": false, "tags": []any{},
	}
	if arr.kind == sonarrKind {
		seasonFolders, _ := declared["season_folders"].(bool)
		entry["enableSeasonFolders"], entry["animeTags"] = seasonFolders, []any{}
	} else {
		entry["minimumAvailability"] = "released"
		if wanted, ok := declared["minimum_availability"]; ok {
			entry["minimumAvailability"] = wanted
		}
	}
	profile, err := s.profileAndFolder(ctx, arr, key, declared)
	if err != nil {
		return err
	}
	setProfileAndFolder(entry, arr, profile, declared["root_folder"])
	return s.api.Send(ctx, "POST", "/api/v1/settings/"+arr.kind, entry, nil)
}

func (s *seerr) initialise(ctx context.Context) error {
	public, err := s.public(ctx)
	if err != nil || public["initialized"] == true {
		return err
	}
	s.env.Change(seerrApp, "initialise")
	return s.api.Send(ctx, "POST", "/api/v1/settings/initialize", nil, nil)
}
