package wiring

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const (
	jellyfinApp      = "jellyfin"
	jellyfinKeyName  = "media-server"
	jellyfinKeyState = "jellyfin.key"
	jellyfinClient   = `MediaBrowser Client="media-server", Device="wiring", DeviceId="media-server-wiring", Version="1"`
)

type jellyfin struct {
	env      Env
	api      *API
	user     string
	password string
	failures []string
}

func Jellyfin(ctx context.Context, env Env) error {
	declared, err := env.Declared("apps.yml")
	if err != nil {
		return err
	}
	config := section(declared[jellyfinApp])
	user, err := env.Setting("JELLYFIN_ADMIN_USER")
	if err != nil {
		return err
	}
	password, err := env.Secret("JELLYFIN_ADMIN_PASSWORD")
	if err != nil {
		return err
	}
	j := &jellyfin{
		env:      env,
		api:      &API{Env: env, Base: env.URL("JELLYFIN_URL", "http://localhost:8096"), Headers: map[string]string{}},
		user:     user,
		password: password,
	}
	serverName := text(config["server_name"])
	if err := j.completeWizard(ctx, serverName); err != nil {
		return err
	}
	if err := j.ensureKey(ctx); err != nil {
		return err
	}
	if serverName != "" {
		if err := j.wireServerName(ctx, serverName); err != nil {
			return err
		}
	}
	if err := j.wireLibraries(ctx, entries(config["libraries"])); err != nil {
		return err
	}
	if len(j.failures) > 0 {
		return Error{Message: strings.Join(j.failures, "; ")}
	}
	return nil
}

func (j *jellyfin) useToken(token string) {
	j.api.Headers["Authorization"] = fmt.Sprintf(`%s, Token="%s"`, jellyfinClient, token)
}

func (j *jellyfin) completeWizard(ctx context.Context, serverName string) error {
	var public map[string]any
	if err := j.api.Get(ctx, "/System/Info/Public", &public); err != nil {
		return err
	}
	if public["StartupWizardCompleted"] == true {
		return nil
	}
	j.env.Change(jellyfinApp, "complete the startup wizard with admin user "+j.user)
	var configuration map[string]any
	if err := j.api.Get(ctx, "/Startup/Configuration", &configuration); err != nil {
		return err
	}
	configuration["ServerName"] = serverName
	steps := []struct {
		path string
		body any
	}{
		{"/Startup/Configuration", configuration},
		{"/Startup/User", map[string]any{"Name": j.user, "Password": j.password}},
		{"/Startup/RemoteAccess", map[string]any{"EnableRemoteAccess": true, "EnableAutomaticPortMapping": false}},
		{"/Startup/Complete", nil},
	}
	for n, step := range steps {
		if n == 1 {
			if err := j.api.Get(ctx, "/Startup/User", nil); err != nil {
				return err
			}
		}
		if err := j.api.Send(ctx, "POST", step.path, step.body, nil); err != nil {
			return err
		}
	}
	return nil
}

func (j *jellyfin) ensureKey(ctx context.Context) error {
	if stored := j.env.State().Remembered(jellyfinKeyState); stored != "" {
		j.useToken(stored)
		if j.api.Get(ctx, "/System/Info", nil) == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if err := j.signIn(ctx); err != nil {
		return err
	}
	key, err := j.mediaServerKey(ctx)
	if err != nil {
		return err
	}
	if key == "" {
		j.env.Change(jellyfinApp, "create API key "+jellyfinKeyName)
		if err := j.api.Send(ctx, "POST", "/Auth/Keys?app="+jellyfinKeyName, nil, nil); err != nil {
			return err
		}
		if key, err = j.mediaServerKey(ctx); err != nil {
			return err
		}
	}
	if err := j.env.State().Remember(jellyfinKeyState, key); err != nil {
		return err
	}
	j.useToken(key)
	return nil
}

func (j *jellyfin) signIn(ctx context.Context) error {
	j.api.Headers["Authorization"] = jellyfinClient
	var session map[string]any
	if err := j.api.Send(ctx, "POST", "/Users/AuthenticateByName", map[string]any{"Username": j.user, "Pw": j.password}, &session); err != nil {
		return Error{Message: fmt.Sprintf("cannot sign in as %s: %v", j.user, err)}
	}
	j.useToken(text(session["AccessToken"]))
	return nil
}

func (j *jellyfin) mediaServerKey(ctx context.Context) (string, error) {
	var keys map[string]any
	if err := j.api.Get(ctx, "/Auth/Keys", &keys); err != nil {
		return "", err
	}
	for _, key := range entries(keys["Items"]) {
		if key["AppName"] == jellyfinKeyName {
			return text(key["AccessToken"]), nil
		}
	}
	return "", nil
}

func (j *jellyfin) wireServerName(ctx context.Context, serverName string) error {
	var configuration map[string]any
	if err := j.api.Get(ctx, "/System/Configuration", &configuration); err != nil {
		return err
	}
	if configuration["ServerName"] == serverName {
		return nil
	}
	j.env.Change(jellyfinApp, fmt.Sprintf("set server name %s -> %s", text(configuration["ServerName"]), serverName))
	configuration["ServerName"] = serverName
	return j.api.Send(ctx, "POST", "/System/Configuration", configuration, nil)
}

func (j *jellyfin) wireLibraries(ctx context.Context, libraries []map[string]any) error {
	var listed []map[string]any
	if err := j.api.Get(ctx, "/Library/VirtualFolders", &listed); err != nil {
		return err
	}
	current := map[string]map[string]any{}
	for _, library := range listed {
		current[text(library["Name"])] = library
	}
	scan := false
	for _, library := range libraries {
		added, err := j.wireLibrary(ctx, library, current[text(library[nameKey])])
		if err != nil {
			return err
		}
		scan = scan || added
	}
	if !scan {
		return nil
	}
	j.env.Change(jellyfinApp, "scan the libraries for what was added")
	return j.api.Send(ctx, "POST", "/Library/Refresh", nil, nil)
}

func (j *jellyfin) wireLibrary(ctx context.Context, library, existing map[string]any) (bool, error) {
	name, kind, path := text(library[nameKey]), text(library["type"]), text(library["path"])
	if existing == nil {
		j.env.Change(jellyfinApp, "add library "+name)
		query := "name=" + url.QueryEscape(name) + "&collectionType=" + url.QueryEscape(kind) + "&paths=" + url.QueryEscape(path) + "&refreshLibrary=false"
		return true, j.api.Send(ctx, "POST", "/Library/VirtualFolders?"+query, map[string]any{"LibraryOptions": map[string]any{"EnableRealtimeMonitor": true}}, nil)
	}
	if text(existing["CollectionType"]) != kind {
		j.failures = append(j.failures, fmt.Sprintf("library %s is %s, not %s; change it in Jellyfin", name, text(existing["CollectionType"]), kind))
		return false, nil
	}
	added := false
	if !contains(list(existing["Locations"]), path) {
		j.env.Change(jellyfinApp, fmt.Sprintf("add %s to library %s", path, name))
		if err := j.api.Send(ctx, "POST", "/Library/VirtualFolders/Paths?refreshLibrary=false", map[string]any{"Name": name, "PathInfo": map[string]any{"Path": path}}, nil); err != nil {
			return false, err
		}
		added = true
	}
	options := section(existing["LibraryOptions"])
	if options["EnableRealtimeMonitor"] != true {
		j.env.Change(jellyfinApp, "turn on real-time monitoring for library "+name)
		options["EnableRealtimeMonitor"] = true
		if err := j.api.Send(ctx, "POST", "/Library/VirtualFolders/LibraryOptions", map[string]any{"Id": existing["ItemId"], "LibraryOptions": options}, nil); err != nil {
			return false, err
		}
	}
	return added, nil
}

func jellyfinKey(env Env) (string, error) {
	if key := env.State().Remembered(jellyfinKeyState); key != "" {
		return key, nil
	}
	return "", Error{Message: "no Jellyfin key in data/volumes/.wiring/jellyfin.key; the jellyfin wiring stores it"}
}
