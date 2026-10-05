package wiring

import (
	"context"
	"fmt"
	"strings"
)

const maintainerrApp = "maintainerr"

type maintainerrArr struct {
	kind, serverName, url, key string
}

var maintainerrArrs = []maintainerrArr{
	{sonarrKind, "Sonarr", "http://sonarr:8989", sonarrKeyName},
	{radarrKind, "Radarr", "http://radarr:7878", radarrKeyName},
}

type maintainerr struct {
	env      Env
	api      *API
	failures []string
}

func Maintainerr(ctx context.Context, env Env) error {
	m := &maintainerr{env: env, api: &API{Env: env, Base: env.URL("MAINTAINERR_URL", "http://localhost:6246"), Headers: map[string]string{}}}
	m.attempt(ctx, func() error {
		key, err := jellyfinKey(env)
		if err != nil {
			return err
		}
		return m.connect(ctx, "jellyfin", "/api/settings/jellyfin", map[string]any{"jellyfin_url": "http://jellyfin:8096", "jellyfin_api_key": key})
	})
	m.attempt(ctx, func() error {
		key, found := readSeerrKey(env)
		if !found {
			return Error{Message: "no Seerr API key in " + seerrSettings(env)}
		}
		return m.connect(ctx, seerrApp, "/api/settings/seerr", map[string]any{"url": "http://seerr:5055", "api_key": key})
	})
	for _, arr := range maintainerrArrs {
		m.attempt(ctx, func() error { return m.connectArr(ctx, arr) })
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(m.failures) > 0 {
		return Error{Message: strings.Join(m.failures, "; ")}
	}
	return nil
}

func (m *maintainerr) attempt(ctx context.Context, step func() error) {
	if ctx.Err() != nil {
		return
	}
	if err := step(); err != nil {
		m.failures = append(m.failures, err.Error())
	}
}

func (m *maintainerr) save(ctx context.Context, method, path string, body map[string]any) error {
	var answer any
	if err := m.api.Send(ctx, method, path, body, &answer); err != nil {
		return err
	}
	if status, ok := answer.(map[string]any); ok && status["status"] != nil && status["status"] != "OK" {
		return Error{Message: fmt.Sprintf("%s %s: %s", method, path, text(status["message"]))}
	}
	return nil
}

func (m *maintainerr) connect(ctx context.Context, name, path string, wanted map[string]any) error {
	var current map[string]any
	if err := m.api.Get(ctx, path, &current); err != nil {
		return err
	}
	for _, key := range sortedKeys(wanted) {
		if !same(current[key], wanted[key]) {
			m.env.Change(maintainerrApp, "connect "+name)
			return m.save(ctx, "POST", path, wanted)
		}
	}
	return nil
}

func (m *maintainerr) connectArr(ctx context.Context, arr maintainerrArr) error {
	key, err := m.env.Secret(arr.key)
	if err != nil {
		return err
	}
	wanted := map[string]any{"serverName": arr.serverName, "url": arr.url, apiKeyField: key}
	var listed []map[string]any
	if err := m.api.Get(ctx, "/api/settings/"+arr.kind, &listed); err != nil {
		return err
	}
	var entry map[string]any
	for _, candidate := range listed {
		if candidate["url"] == arr.url || candidate["serverName"] == arr.serverName {
			entry = candidate
			break
		}
	}
	if entry == nil {
		m.env.Change(maintainerrApp, "connect "+arr.kind)
		return m.save(ctx, "POST", "/api/settings/"+arr.kind, wanted)
	}
	for _, key := range sortedKeys(wanted) {
		if !same(entry[key], wanted[key]) {
			m.env.Change(maintainerrApp, "reconnect "+arr.kind)
			return m.save(ctx, "PUT", "/api/settings/"+arr.kind+"/"+show(entry["id"]), wanted)
		}
	}
	return nil
}
