package wiring

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const (
	prowlarrApp        = "prowlarr"
	prowlarrSeenByApps = "http://gluetun:9696"
	unreachableIndexer = "Unable to connect to indexer"
	indexerKind        = "indexer"
	syncCommand        = "ApplicationIndexerSync"
	syncPolls          = 12
	syncPoll           = 5
)

type prowlarr struct {
	env      Env
	api      *API
	changed  bool
	failures []string
}

func prowlarrAPI(env Env) *API {
	return &API{Env: env, Base: env.URL("PROWLARR_URL", "http://localhost:9696"), Headers: map[string]string{"X-Api-Key": env.Secrets["PROWLARR_API_KEY"]}}
}

func Prowlarr(ctx context.Context, env Env) error {
	declared, err := env.Declared("prowlarr.yml")
	if err != nil {
		return err
	}
	p := &prowlarr{env: env, api: prowlarrAPI(env)}
	tags, err := p.wireProxies(ctx, entries(declared["indexer_proxies"]))
	if err != nil {
		return err
	}
	if err := p.wireIndexers(ctx, entries(declared["indexers"]), tags); err != nil {
		return err
	}
	if err := p.wireApplications(ctx, entries(declared["applications"])); err != nil {
		return err
	}
	if p.changed {
		if err := askToSync(ctx, p.api); err != nil {
			return err
		}
	}
	if len(p.failures) > 0 {
		return Error{Message: strings.Join(p.failures, "; ")}
	}
	return nil
}

func askToSync(ctx context.Context, api *API) error {
	return api.Send(ctx, "POST", "/api/v1/command", map[string]any{nameKey: syncCommand}, nil)
}

func (p *prowlarr) change(line string) {
	p.env.Change(prowlarrApp, line)
	p.changed = true
}

func (p *prowlarr) list(ctx context.Context, path string) ([]map[string]any, error) {
	var items []map[string]any
	return items, p.api.Get(ctx, path, &items)
}

func (p *prowlarr) schema(ctx context.Context, kind, key, value, what string) (map[string]any, error) {
	schemas, err := p.list(ctx, "/api/v1/"+kind+"/schema")
	if err != nil {
		return nil, err
	}
	for _, schema := range schemas {
		if schema[key] == value {
			return schema, nil
		}
	}
	p.failures = append(p.failures, fmt.Sprintf("%s: no %s type %s", what, kind, value))
	return nil, nil
}

func (p *prowlarr) save(ctx context.Context, kind string, item map[string]any) (bool, error) {
	path := "/api/v1/" + kind + "?forceSave=true"
	method := "POST"
	if id, ok := item["id"]; ok {
		method, path = "PUT", "/api/v1/"+kind+"/"+show(id)+"?forceSave=true"
	}
	err := p.api.Send(ctx, method, path, item, nil)
	if _, refused := err.(Error); !refused {
		return err == nil, err
	}
	if kind == indexerKind && strings.Contains(err.Error(), unreachableIndexer) {
		p.env.Change(prowlarrApp, fmt.Sprintf("could not reach indexer %s; it is added at a later update once its site answers from this VPN location", item[nameKey]))
		return false, nil
	}
	p.failures = append(p.failures, fmt.Sprintf("could not save %s: %v", item[nameKey], err))
	return false, nil
}

func (p *prowlarr) tagID(ctx context.Context, label string) (any, error) {
	tags, err := p.list(ctx, "/api/v1/tag")
	if err != nil {
		return nil, err
	}
	for _, tag := range tags {
		if tag["label"] == label {
			return tag["id"], nil
		}
	}
	p.change("add tag " + label)
	var tag map[string]any
	if err := p.api.Send(ctx, "POST", "/api/v1/tag", map[string]any{"label": label}, &tag); err != nil {
		return nil, err
	}
	return tag["id"], nil
}

func (p *prowlarr) wireProxies(ctx context.Context, proxies []map[string]any) (map[string]any, error) {
	tags := map[string]any{}
	current, err := p.list(ctx, "/api/v1/indexerproxy")
	if err != nil {
		return nil, err
	}
	for _, proxy := range proxies {
		name := text(proxy[nameKey])
		if tags[name], err = p.tagID(ctx, strings.ToLower(name)); err != nil {
			return nil, err
		}
		if err := p.wireProxy(ctx, proxy, byName(current, name), tags[name]); err != nil {
			return nil, err
		}
	}
	return tags, nil
}

func (p *prowlarr) wireProxy(ctx context.Context, proxy, item map[string]any, tag any) error {
	name, host := text(proxy[nameKey]), proxy["host"]
	if item == nil {
		kind := text(proxy["type"])
		if kind == "" {
			kind = name
		}
		schema, err := p.schema(ctx, "indexerproxy", "implementation", kind, "proxy "+name)
		if err != nil || schema == nil {
			return err
		}
		schema[nameKey], schema["tags"] = name, []any{tag}
		setField(schema, "host", host)
		p.change("add proxy " + name)
		_, err = p.save(ctx, "indexerproxy", schema)
		return err
	}
	dirty := false
	if !same(field(item, "host"), host) {
		p.change(fmt.Sprintf("set proxy %s host %s -> %s", name, show(field(item, "host")), show(host)))
		setField(item, "host", host)
		dirty = true
	}
	if !contains(list(item["tags"]), tag) {
		p.change(fmt.Sprintf("tag proxy %s %s", name, strings.ToLower(name)))
		item["tags"] = append(list(item["tags"]), tag)
		dirty = true
	}
	if !dirty {
		return nil
	}
	_, err := p.save(ctx, "indexerproxy", item)
	return err
}

func (p *prowlarr) wireIndexers(ctx context.Context, indexers []map[string]any, tags map[string]any) error {
	current, err := p.list(ctx, "/api/v1/indexer")
	if err != nil {
		return err
	}
	profiles, err := p.list(ctx, "/api/v1/appprofile")
	if err != nil {
		return err
	}
	var profile any
	if len(profiles) > 0 {
		profile = profiles[0]["id"]
	}
	for _, indexer := range indexers {
		name, proxy := text(indexer[nameKey]), text(indexer["proxy"])
		var wanted []any
		if proxy != "" {
			tag, declared := tags[proxy]
			if !declared {
				p.failures = append(p.failures, fmt.Sprintf("indexer %s uses proxy %s, which is not declared", name, proxy))
				continue
			}
			wanted = []any{tag}
		}
		if err := p.wireIndexer(ctx, indexer, byName(current, name), profile, wanted); err != nil {
			return err
		}
	}
	return nil
}

func (p *prowlarr) wireIndexer(ctx context.Context, indexer, item map[string]any, profile any, tags []any) error {
	if item == nil {
		return p.addIndexer(ctx, indexer, profile, tags)
	}
	if !p.indexerDrifted(indexer, item, tags) {
		return nil
	}
	_, err := p.save(ctx, indexerKind, item)
	return err
}

func (p *prowlarr) addIndexer(ctx context.Context, indexer map[string]any, profile any, tags []any) error {
	name, fields := text(indexer[nameKey]), section(indexer["fields"])
	schema, err := p.schema(ctx, indexerKind, "definitionName", text(indexer["definition"]), "indexer "+name)
	if err != nil || schema == nil {
		return err
	}
	schema[nameKey], schema["enable"], schema["appProfileId"] = name, true, profile
	schema["tags"] = append([]any{}, tags...)
	if priority, ok := indexer["priority"]; ok {
		schema["priority"] = priority
	}
	for _, key := range sortedKeys(fields) {
		setField(schema, key, fields[key])
	}
	p.change("add indexer " + name)
	_, err = p.save(ctx, indexerKind, schema)
	return err
}

func (p *prowlarr) indexerDrifted(indexer, item map[string]any, tags []any) bool {
	name, fields := text(indexer[nameKey]), section(indexer["fields"])
	dirty := false
	if priority, ok := indexer["priority"]; ok && !same(item["priority"], priority) {
		p.change(fmt.Sprintf("set indexer %s priority %s -> %s", name, show(item["priority"]), show(priority)))
		item["priority"] = priority
		dirty = true
	}
	for _, key := range sortedKeys(fields) {
		if !same(field(item, key), fields[key]) {
			p.change(fmt.Sprintf("set indexer %s %s %s -> %s", name, key, show(field(item, key)), show(fields[key])))
			setField(item, key, fields[key])
			dirty = true
		}
	}
	for _, tag := range tags {
		if !contains(list(item["tags"]), tag) {
			p.change(fmt.Sprintf("tag indexer %s for proxy %s", name, text(indexer["proxy"])))
			item["tags"] = append(list(item["tags"]), tag)
			dirty = true
		}
	}
	return dirty
}

type setting struct {
	name  string
	value any
}

func (p *prowlarr) wireApplications(ctx context.Context, applications []map[string]any) error {
	current, err := p.list(ctx, "/api/v1/applications")
	if err != nil {
		return err
	}
	for _, application := range applications {
		name, keyName := text(application[nameKey]), text(application["api_key"])
		key := p.env.Secrets[keyName]
		if key == "" {
			p.failures = append(p.failures, fmt.Sprintf("application %s: %s is not in the app secrets", name, keyName))
			continue
		}
		wanted := []setting{{"baseUrl", application["url"]}, {"prowlarrUrl", prowlarrSeenByApps}}
		if categories, ok := application["sync_categories"]; ok {
			wanted = append(wanted, setting{"syncCategories", categories})
		}
		if err := p.wireApplication(ctx, application, byName(current, name), wanted, key); err != nil {
			return err
		}
	}
	return nil
}

func (p *prowlarr) wireApplication(ctx context.Context, application, item map[string]any, wanted []setting, key string) error {
	name := text(application[nameKey])
	keyState, keyFingerprint := "prowlarr-application-"+name+".sha256", Fingerprint(name, key)
	if item == nil {
		schema, err := p.applicationSchema(ctx, application)
		if err != nil || schema == nil {
			return err
		}
		item = schema
	} else if !p.applicationDrifted(name, item, wanted, p.env.State().Remembered(keyState) != keyFingerprint) {
		return nil
	}
	for _, s := range wanted {
		setField(item, s.name, s.value)
	}
	setField(item, "apiKey", key)
	saved, err := p.save(ctx, "applications", item)
	if err != nil || !saved {
		return err
	}
	return p.env.State().Remember(keyState, keyFingerprint)
}

func (p *prowlarr) applicationSchema(ctx context.Context, application map[string]any) (map[string]any, error) {
	name, kind := text(application[nameKey]), text(application["type"])
	if kind == "" {
		kind = name
	}
	schema, err := p.schema(ctx, "applications", "implementation", kind, "application "+name)
	if err != nil || schema == nil {
		return nil, err
	}
	schema[nameKey] = name
	p.change("add application " + name)
	return schema, nil
}

func (p *prowlarr) applicationDrifted(name string, item map[string]any, wanted []setting, keyChanged bool) bool {
	dirty := false
	for _, s := range wanted {
		if !same(field(item, s.name), s.value) {
			p.change(fmt.Sprintf("set application %s %s %s -> %s", name, s.name, show(field(item, s.name)), show(s.value)))
			dirty = true
		}
	}
	if keyChanged {
		p.change(fmt.Sprintf("set application %s api key", name))
		dirty = true
	}
	return dirty
}

func addressFromThisMachine(env Env, name, declared string) string {
	if address := env.Settings[strings.ToUpper(name)+"_URL"]; address != "" {
		return address
	}
	parsed, err := url.Parse(declared)
	if err != nil {
		return declared
	}
	port := parsed.Port()
	parsed.Host = "localhost"
	if port != "" {
		parsed.Host += ":" + port
	}
	return parsed.String()
}
