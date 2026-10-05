package wiring

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type shortApp struct {
	application map[string]any
	count       int
}

func ProwlarrSync(ctx context.Context, env Env) error {
	declared, err := env.Declared("prowlarr.yml")
	if err != nil {
		return err
	}
	applications := entries(declared["applications"])
	api, err := prowlarrAPI(env)
	if err != nil {
		return err
	}
	wanted, err := wantedIndexers(ctx, api, applications)
	if err != nil {
		return err
	}
	short, err := shortOfIndexers(ctx, env, applications, wanted)
	if err != nil || len(short) == 0 {
		return err
	}
	described := make([]string, len(short))
	for n, s := range short {
		name := text(s.application[nameKey])
		described[n] = fmt.Sprintf("%s has %d of %d", name, s.count, wanted[name])
	}
	env.Change(prowlarrApp, "sync indexers again: "+strings.Join(described, ", "))
	if err := askToSync(ctx, api); err != nil {
		return err
	}
	if short, err = waitForSync(ctx, env, short, wanted); err != nil {
		return err
	}
	for _, s := range short {
		name := text(s.application[nameKey])
		env.Say(fmt.Sprintf("%s: %s still has %d of prowlarr's %d indexers; prowlarr will retry on its own schedule", prowlarrApp, name, s.count, wanted[name]))
	}
	return nil
}

func wantedIndexers(ctx context.Context, api *API, applications []map[string]any) (map[string]int, error) {
	var indexers, configured []map[string]any
	if err := api.Get(ctx, "/api/v1/indexer", &indexers); err != nil {
		return nil, err
	}
	if err := api.Get(ctx, "/api/v1/applications", &configured); err != nil {
		return nil, err
	}
	wanted := map[string]int{}
	for _, application := range applications {
		name := text(application[nameKey])
		wanted[name] = indexersSyncedTo(name, indexers, configured)
	}
	return wanted, nil
}

func waitForSync(ctx context.Context, env Env, short []shortApp, wanted map[string]int) ([]shortApp, error) {
	for poll := 0; len(short) > 0 && poll < syncPolls; poll++ {
		if err := env.Pause(ctx, syncPoll*time.Second); err != nil {
			return nil, err
		}
		still := make([]map[string]any, len(short))
		for n, s := range short {
			still[n] = s.application
		}
		var err error
		if short, err = shortOfIndexers(ctx, env, still, wanted); err != nil {
			return nil, err
		}
	}
	return short, nil
}

func shortOfIndexers(ctx context.Context, env Env, applications []map[string]any, wanted map[string]int) ([]shortApp, error) {
	var short []shortApp
	for _, application := range applications {
		count, err := indexersIn(ctx, env, application)
		if err != nil {
			return nil, err
		}
		if count < wanted[text(application[nameKey])] {
			short = append(short, shortApp{application: application, count: count})
		}
	}
	return short, nil
}

func indexersIn(ctx context.Context, env Env, application map[string]any) (int, error) {
	name := text(application[nameKey])
	key, err := env.Secret(text(application["api_key"]))
	if err != nil {
		return 0, err
	}
	api := &API{Env: env, Base: addressFromThisMachine(env, name, text(application["url"])), Headers: map[string]string{apiKeyHeader: key}}
	var indexers []map[string]any
	if err := api.Get(ctx, "/api/v3/indexer", &indexers); err != nil {
		return 0, err
	}
	count := 0
	for _, indexer := range indexers {
		if strings.HasSuffix(text(indexer[nameKey]), "(Prowlarr)") {
			count++
		}
	}
	return count, nil
}

func indexersSyncedTo(name string, indexers, configured []map[string]any) int {
	categories := map[string]bool{}
	if application := byName(configured, name); application != nil {
		for _, category := range list(field(application, "syncCategories")) {
			categories[show(category)] = true
		}
	}
	count := 0
	for _, indexer := range indexers {
		if indexer["enable"] != true {
			continue
		}
		offered, known := categoriesOf(indexer)
		if !known || overlaps(offered, categories) {
			count++
		}
	}
	return count
}

func categoriesOf(indexer map[string]any) ([]string, bool) {
	capabilities := section(indexer["capabilities"])
	categories, known := capabilities["categories"]
	if !known || categories == nil {
		return nil, false
	}
	var ids []string
	for _, category := range entries(categories) {
		ids = append(ids, show(category["id"]))
		for _, sub := range entries(category["subCategories"]) {
			ids = append(ids, show(sub["id"]))
		}
	}
	return ids, true
}

func overlaps(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}
