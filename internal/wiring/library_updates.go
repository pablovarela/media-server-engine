package wiring

import (
	"context"
	"fmt"
	"strings"
)

const jellyfinConnectionName = "Emby / Jellyfin"

var eventsLeftOff = map[string]bool{"onHealthIssue": true, "onHealthRestored": true, "onManualInteractionRequired": true}

type libraryArr struct {
	kind, variable, fallback, key string
}

var libraryArrs = []libraryArr{
	{sonarrKind, "SONARR_URL", "http://localhost:8989", sonarrKeyName},
	{radarrKind, "RADARR_URL", "http://localhost:7878", radarrKeyName},
}

func (a libraryArr) wanted() []setting {
	return []setting{{"host", "jellyfin"}, {portField, 8096}, {"updateLibrary", true}, {"mapFrom", nil}, {"mapTo", nil}}
}

func LibraryUpdates(ctx context.Context, env Env) error {
	key, err := jellyfinKey(env)
	if err != nil {
		return err
	}
	var failures []string
	for _, arr := range libraryArrs {
		if err := connectArrToJellyfin(ctx, env, arr, key); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures = append(failures, fmt.Sprintf("%s: %v", arr.kind, err))
		}
	}
	if len(failures) > 0 {
		return Error{Message: strings.Join(failures, "; ")}
	}
	return nil
}

func connectArrToJellyfin(ctx context.Context, env Env, arr libraryArr, jellyfin string) error {
	key, err := env.Secret(arr.key)
	if err != nil {
		return err
	}
	return connectToJellyfin(ctx, env, arr, &API{Env: env, Base: env.URL(arr.variable, arr.fallback), Headers: map[string]string{apiKeyHeader: key}}, jellyfin)
}

func connectToJellyfin(ctx context.Context, env Env, arr libraryArr, api *API, key string) error {
	keyState := arr.kind + "-jellyfin-connection.sha256"
	var notifications []map[string]any
	if err := api.Get(ctx, "/api/v3/notification", &notifications); err != nil {
		return err
	}
	current := withImplementation(notifications, "MediaBrowser")
	if current == nil {
		return addJellyfinConnection(ctx, env, arr, api, key)
	}
	dirty := false
	for _, s := range arr.wanted() {
		if !same(field(current, s.name), s.value) {
			env.Change(arr.kind, fmt.Sprintf("set the Jellyfin connection %s %s -> %s", s.name, show(field(current, s.name)), show(s.value)))
			setField(current, s.name, s.value)
			dirty = true
		}
	}
	if env.State().Remembered(keyState) != Fingerprint(key) {
		env.Change(arr.kind, "set the Jellyfin connection api key")
		dirty = true
	}
	if !dirty {
		return nil
	}
	setField(current, apiKeyField, key)
	if err := api.Send(ctx, "PUT", "/api/v3/notification/"+show(current["id"])+"?forceSave=true", current, nil); err != nil {
		return err
	}
	return env.State().Remember(keyState, Fingerprint(key))
}

func addJellyfinConnection(ctx context.Context, env Env, arr libraryArr, api *API, key string) error {
	var schemas []map[string]any
	if err := api.Get(ctx, "/api/v3/notification/schema", &schemas); err != nil {
		return err
	}
	item := withImplementation(schemas, "MediaBrowser")
	if item == nil {
		return Error{Message: arr.kind + " has no MediaBrowser connection type"}
	}
	item[nameKey] = jellyfinConnectionName
	for event := range item {
		if strings.HasPrefix(event, "on") {
			item[event] = !eventsLeftOff[event]
		}
	}
	for _, s := range arr.wanted() {
		setField(item, s.name, s.value)
	}
	setField(item, apiKeyField, key)
	env.Change(arr.kind, "add the Jellyfin connection")
	if err := api.Send(ctx, "POST", "/api/v3/notification?forceSave=true", item, nil); err != nil {
		return err
	}
	return env.State().Remember(arr.kind+"-jellyfin-connection.sha256", Fingerprint(key))
}

func withImplementation(items []map[string]any, implementation string) map[string]any {
	for _, item := range items {
		if item["implementation"] == implementation {
			return item
		}
	}
	return nil
}
