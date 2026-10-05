package wiring

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const prowlarrYML = `indexer_proxies:
- name: FlareSolverr
  host: http://localhost:8191/
indexers:
- name: The Pirate Bay
  definition: thepiratebay
  fields:
    apiurl: apibay.org
  proxy: FlareSolverr
  priority: 25
applications:
- name: Sonarr
  url: http://sonarr:8989
  api_key: SONARR_API_KEY
  sync_categories: [5000, 5010, 5020, 5030, 5040, 5045, 5050]
- name: Radarr
  url: http://radarr:7878
  api_key: RADARR_API_KEY
`

const (
	sonarrCategories = `[5000, 5010, 5020, 5030, 5040, 5045, 5050]`
	radarrCategories = `[2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080]`
	proxySchema      = `{"implementation": "FlareSolverr", "name": "", "tags": [], "fields": [{"name": "host", "value": "http://localhost:8191/"}, {"name": "requestTimeout", "value": 60}]}`
	indexerSchema    = `{"definitionName": "thepiratebay", "enable": true, "appProfileId": 0, "priority": 25, "tags": [], "fields": [{"name": "definitionFile", "value": "thepiratebay"}, {"name": "apiurl", "value": null}]}`
	wiredProxy       = `{"id": 1, "implementation": "FlareSolverr", "name": "FlareSolverr", "tags": [1], "fields": [{"name": "host", "value": "http://localhost:8191/"}, {"name": "requestTimeout", "value": 60}]}`
	wiredIndexer     = `{"id": 1, "definitionName": "thepiratebay", "name": "The Pirate Bay", "enable": true, "appProfileId": 1, "priority": 25, "tags": [1], "fields": [{"name": "definitionFile", "value": "thepiratebay"}, {"name": "apiurl", "value": "apibay.org"}]}`
)

var (
	applicationSchemas = `[` +
		`{"implementation": "Sonarr", "name": "", "fields": [{"name": "prowlarrUrl", "value": "http://localhost:9696"}, {"name": "baseUrl", "value": "http://localhost"}, {"name": "apiKey", "value": null}, {"name": "syncCategories", "value": ` + sonarrCategories + `}]},` +
		`{"implementation": "Radarr", "name": "", "fields": [{"name": "prowlarrUrl", "value": "http://localhost:9696"}, {"name": "baseUrl", "value": "http://localhost"}, {"name": "apiKey", "value": null}, {"name": "syncCategories", "value": ` + radarrCategories + `}]}]`
	wiredSonarr       = `{"id": 1, "name": "Sonarr", "implementation": "Sonarr", "fields": [{"name": "prowlarrUrl", "value": "http://gluetun:9696"}, {"name": "baseUrl", "value": "http://sonarr:8989"}, {"name": "apiKey", "value": "********"}, {"name": "syncCategories", "value": ` + sonarrCategories + `}]}`
	wiredApplications = `[` + wiredSonarr + `, {"id": 2, "name": "Radarr", "implementation": "Radarr", "fields": [{"name": "prowlarrUrl", "value": "http://gluetun:9696"}, {"name": "baseUrl", "value": "http://radarr:7878"}, {"name": "apiKey", "value": "********"}, {"name": "syncCategories", "value": ` + radarrCategories + `}]}]`
)

var prowlarrSecrets = map[string]string{"SONARR_API_KEY": "sonarr-key", "RADARR_API_KEY": "radarr-key", "PROWLARR_API_KEY": "prowlarr-key"}

func prowlarrAnswering(r *routes, proxies, indexers, applications, tags string) *routes {
	r.on("GET", "/api/v1/indexerproxy", ok(proxies))
	r.on("GET", "/api/v1/indexerproxy/schema", ok("["+proxySchema+"]"))
	r.on("GET", "/api/v1/indexer", ok(indexers))
	r.on("GET", "/api/v1/indexer/schema", ok("["+indexerSchema+"]"))
	r.on("GET", "/api/v1/applications", ok(applications))
	r.on("GET", "/api/v1/applications/schema", ok(applicationSchemas))
	r.on("GET", "/api/v1/appprofile", ok(`[{"id": 1, "name": "Standard"}]`))
	r.on("GET", "/api/v1/tag", ok(tags))
	r.on("POST", "/api/v1/tag", ok(`{"id": 1, "label": "flaresolverr"}`))
	for _, kind := range []string{"indexerproxy", "indexer", "applications"} {
		r.on("POST", "/api/v1/"+kind+"?forceSave=true")
	}
	for _, path := range []string{"indexerproxy/1", "indexer/1", "applications/1", "applications/2"} {
		r.on("PUT", "/api/v1/"+path+"?forceSave=true")
	}
	return r.on("POST", "/api/v1/command")
}

func prowlarrFresh(r *routes) *routes { return prowlarrAnswering(r, "[]", "[]", "[]", "[]") }

func prowlarrWired(r *routes, indexer string) *routes {
	if indexer == "" {
		indexer = wiredIndexer
	}
	return prowlarrAnswering(r, "["+wiredProxy+"]", "["+indexer+"]", wiredApplications, `[{"id": 1, "label": "flaresolverr"}]`)
}

func prowlarrEnv(t *testing.T, r *routes, declared string, secrets map[string]string) (Env, *said) {
	t.Helper()
	if secrets == nil {
		secrets = prowlarrSecrets
	}
	env, out := testEnv(t, r.client(), secrets)
	env.Settings["PROWLARR_URL"] = "http://prowlarr"
	require.NoError(t, os.WriteFile(filepath.Join(env.Config, "prowlarr.yml"), []byte(declared), 0o644))
	return env, out
}

func rememberAppliedKeys(t *testing.T, env Env, sonarr, radarr string) {
	t.Helper()
	for name, key := range map[string]string{"Sonarr": sonarr, "Radarr": radarr} {
		require.NoError(t, env.State().Remember("prowlarr-application-"+name+".sha256", Fingerprint(name, key)))
	}
}

func valueOf(item map[string]any, name string) any {
	for _, entry := range item["fields"].([]any) {
		if f := entry.(map[string]any); f["name"] == name {
			return f["value"]
		}
	}
	return nil
}

func numbers(t *testing.T, list string) []any {
	t.Helper()
	var values []any
	require.NoError(t, yaml.Unmarshal([]byte(list), &values))
	out := make([]any, len(values))
	for n, v := range values {
		out[n] = float64(v.(int))
	}
	return out
}

func TestAFreshProwlarrGetsTheProxyIndexersAndApplicationsThenSyncs(t *testing.T) {
	r := prowlarrFresh(newRoutes(t))
	env, out := prowlarrEnv(t, r, prowlarrYML, nil)

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Equal(t, []string{
		"POST /api/v1/tag", "POST /api/v1/indexerproxy?forceSave=true", "POST /api/v1/indexer?forceSave=true",
		"POST /api/v1/applications?forceSave=true", "POST /api/v1/applications?forceSave=true", "POST /api/v1/command",
	}, r.writes())
	assert.Equal(t, "http://localhost:8191/", valueOf(r.sentBody("POST", "/api/v1/indexerproxy?forceSave=true"), "host"))
	indexer := r.sentBody("POST", "/api/v1/indexer?forceSave=true")
	assert.Equal(t, []any{float64(25), []any{float64(1)}, float64(1), "apibay.org"}, []any{indexer["priority"], indexer["tags"], indexer["appProfileId"], valueOf(indexer, "apiurl")})
	applications := r.sentBodies("POST", "/api/v1/applications?forceSave=true")
	assert.Equal(t, "sonarr-key", valueOf(applications[0], "apiKey"))
	assert.Equal(t, "http://gluetun:9696", valueOf(applications[0], "prowlarrUrl"))
	assert.Equal(t, numbers(t, sonarrCategories), valueOf(applications[0], "syncCategories"))
	assert.Equal(t, "http://radarr:7878", valueOf(applications[1], "baseUrl"))
	assert.Equal(t, numbers(t, radarrCategories), valueOf(applications[1], "syncCategories"))
	assert.Equal(t, map[string]any{"name": "ApplicationIndexerSync"}, r.sentBody("POST", "/api/v1/command"))
	assert.Contains(t, out.lines, "prowlarr: add indexer The Pirate Bay")
}

func TestTheWiringSignsItsRequestsWithProwlarrsOwnKey(t *testing.T) {
	r := prowlarrWired(newRoutes(t), "")
	env, _ := prowlarrEnv(t, r, prowlarrYML, nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	for _, request := range r.requests {
		assert.Equal(t, "prowlarr-key", request.headers.Get("X-Api-Key"))
	}
}

func TestAnAlreadyWiredProwlarrIsLeftUntouchedIncludingIndexersItWasNotToldAbout(t *testing.T) {
	other := strings.Replace(strings.Replace(wiredIndexer, `"id": 1`, `"id": 9`, 1), "The Pirate Bay", "Added by hand", 1)
	r := prowlarrAnswering(newRoutes(t), "["+wiredProxy+"]", "["+wiredIndexer+","+other+"]", wiredApplications, `[{"id": 1, "label": "flaresolverr"}]`)
	env, out := prowlarrEnv(t, r, prowlarrYML, nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestADriftedPriorityIsCorrectedWithOneWriteThenSynced(t *testing.T) {
	r := prowlarrWired(newRoutes(t), strings.Replace(wiredIndexer, `"priority": 25`, `"priority": 50`, 1))
	env, out := prowlarrEnv(t, r, prowlarrYML, nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v1/indexer/1?forceSave=true", "POST /api/v1/command"}, r.writes())
	assert.Equal(t, float64(25), r.sentBody("PUT", "/api/v1/indexer/1?forceSave=true")["priority"])
	assert.Contains(t, out.lines, "prowlarr: set indexer The Pirate Bay priority 50 -> 25")
}

func TestAnIndexerThatLostItsProxyTagGetsItBack(t *testing.T) {
	r := prowlarrWired(newRoutes(t), strings.Replace(wiredIndexer, `"tags": [1]`, `"tags": []`, 1))
	env, _ := prowlarrEnv(t, r, prowlarrYML, nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Equal(t, []any{float64(1)}, r.sentBody("PUT", "/api/v1/indexer/1?forceSave=true")["tags"])
}

func TestApplicationKeysAreAppliedOnceWhenNoAppliedKeyIsRemembered(t *testing.T) {
	r := prowlarrWired(newRoutes(t), "")
	env, _ := prowlarrEnv(t, r, prowlarrYML, nil)

	require.NoError(t, Prowlarr(context.Background(), env))

	var puts []string
	for _, write := range r.writes() {
		if strings.HasPrefix(write, "PUT /api/v1/applications/") {
			puts = append(puts, write)
		}
	}
	assert.Equal(t, []string{"PUT /api/v1/applications/1?forceSave=true", "PUT /api/v1/applications/2?forceSave=true"}, puts)
	assert.Equal(t, "sonarr-key", valueOf(r.sentBody("PUT", "/api/v1/applications/1?forceSave=true"), "apiKey"))
	info, err := os.Stat(filepath.Join(env.State().Dir, "prowlarr-application-Sonarr.sha256"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	r.requests = nil

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Empty(t, r.writes())
}

func TestARotatedKeyReachesOnlyItsApplication(t *testing.T) {
	r := prowlarrWired(newRoutes(t), "")
	env, _ := prowlarrEnv(t, r, prowlarrYML, map[string]string{"SONARR_API_KEY": "rotated", "RADARR_API_KEY": "radarr-key", "PROWLARR_API_KEY": "prowlarr-key"})
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Equal(t, []string{"PUT /api/v1/applications/1?forceSave=true", "POST /api/v1/command"}, r.writes())
	assert.Equal(t, "rotated", valueOf(r.sentBody("PUT", "/api/v1/applications/1?forceSave=true"), "apiKey"))
}

func TestAnItemProwlarrRejectsFailsTheStepAfterTheRestIsWired(t *testing.T) {
	r := prowlarrFresh(newRoutes(t))
	r.on("POST", "/api/v1/indexer?forceSave=true", failing(400, `[{"errorMessage": "Invalid indexer settings"}]`))
	env, _ := prowlarrEnv(t, r, prowlarrYML, nil)

	err := Prowlarr(context.Background(), env)

	require.Error(t, err)
	assert.Regexp(t, `could not save The Pirate Bay: .*Invalid indexer settings`, err.Error())
	assert.Len(t, r.sentBodies("POST", "/api/v1/applications?forceSave=true"), 2)
}

func TestAnIndexerProwlarrCannotReachIsReportedAndAddedAtALaterUpdate(t *testing.T) {
	r := prowlarrFresh(newRoutes(t))
	r.on("POST", "/api/v1/indexer?forceSave=true", failing(400, `[{"errorMessage": "Unable to connect to indexer. Unexpected response status UnavailableForLegalReasons"}]`))
	env, out := prowlarrEnv(t, r, prowlarrYML, nil)

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Contains(t, out.lines, "prowlarr: could not reach indexer The Pirate Bay; it is added at a later update once its site answers from this VPN location")
	assert.Len(t, r.sentBodies("POST", "/api/v1/applications?forceSave=true"), 2)
}

func TestAnIndexerNamingAnUndeclaredProxyFailsTheStepAfterTheApplicationsAreWired(t *testing.T) {
	r := prowlarrFresh(newRoutes(t))
	env, _ := prowlarrEnv(t, r, strings.Replace(prowlarrYML, "  proxy: FlareSolverr", "  proxy: Flaresolver", 1), nil)

	err := Prowlarr(context.Background(), env)

	assert.EqualError(t, err, "indexer The Pirate Bay uses proxy Flaresolver, which is not declared")
	assert.Len(t, r.sentBodies("POST", "/api/v1/applications?forceSave=true"), 2)
}

func TestAWrongProwlarrAPIKeyFailsWithProwlarrsAnswer(t *testing.T) {
	r := newRoutes(t).on("GET", "/api/v1/indexerproxy", failing(401, "Unauthorized"))
	env, _ := prowlarrEnv(t, r, prowlarrYML, nil)

	err := Prowlarr(context.Background(), env)

	assert.EqualError(t, err, "GET /api/v1/indexerproxy answered 401: Unauthorized")
}

func TestTheTemplateDeclaresIndexersProwlarrKnowsBehindTheDeclaredProxy(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "config-template", "prowlarr.yml"))
	require.NoError(t, err)
	var template struct {
		Proxies  []struct{ Name string } `yaml:"indexer_proxies"`
		Indexers []struct {
			Name  string
			Proxy string
		}
	}
	require.NoError(t, yaml.Unmarshal(content, &template))
	names := map[string]bool{}
	for _, indexer := range template.Indexers {
		names[indexer.Name] = true
	}
	for _, name := range []string{"1337x", "The Pirate Bay", "YTS", "LimeTorrents"} {
		assert.True(t, names[name], name)
	}
	proxies := map[string]bool{"": true}
	for _, proxy := range template.Proxies {
		proxies[proxy.Name] = true
	}
	for _, indexer := range template.Indexers {
		assert.True(t, proxies[indexer.Proxy], indexer.Name)
	}
}

func TestATypeProwlarrDoesNotKnowFailsTheStepAfterTheRestIsWired(t *testing.T) {
	tests := map[string]struct {
		from, to, message, stillWritten string
	}{
		"indexer definition":   {"definition: thepiratebay", "definition: thepiratebayy", "indexer The Pirate Bay: no indexer type thepiratebayy", "POST /api/v1/applications?forceSave=true"},
		"proxy type":           {"  host: http://localhost:8191/", "  host: http://localhost:8191/\n  type: Flaresolver", "proxy FlareSolverr: no indexerproxy type Flaresolver", "POST /api/v1/applications?forceSave=true"},
		"application type":     {"  api_key: RADARR_API_KEY", "  api_key: RADARR_API_KEY\n  type: Radar", "application Radarr: no applications type Radar", "POST /api/v1/indexer?forceSave=true"},
		"application key name": {"  api_key: SONARR_API_KEY", "  api_key: SONAR_API_KEY", "application Sonarr: SONAR_API_KEY is not in the app secrets", "POST /api/v1/applications?forceSave=true"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := prowlarrFresh(newRoutes(t))
			env, _ := prowlarrEnv(t, r, strings.Replace(prowlarrYML, tt.from, tt.to, 1), nil)

			err := Prowlarr(context.Background(), env)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.message)
			assert.Contains(t, r.writes(), tt.stillWritten)
		})
	}
}

func TestAProxyHostChangedInTheConfigIsUpdated(t *testing.T) {
	r := prowlarrWired(newRoutes(t), "")
	env, out := prowlarrEnv(t, r, strings.Replace(prowlarrYML, "http://localhost:8191/", "http://flaresolverr:8191/", 1), nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Equal(t, "http://flaresolverr:8191/", valueOf(r.sentBody("PUT", "/api/v1/indexerproxy/1?forceSave=true"), "host"))
	assert.Contains(t, out.lines, "prowlarr: set proxy FlareSolverr host http://localhost:8191/ -> http://flaresolverr:8191/")
}

func TestAnIndexerFieldChangedInTheConfigIsUpdated(t *testing.T) {
	r := prowlarrWired(newRoutes(t), "")
	env, _ := prowlarrEnv(t, r, strings.Replace(prowlarrYML, "apiurl: apibay.org", "apiurl: apibay.example", 1), nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	assert.Equal(t, "apibay.example", valueOf(r.sentBody("PUT", "/api/v1/indexer/1?forceSave=true"), "apiurl"))
}

func TestAnApplicationAddressChangedInTheConfigIsUpdatedWithItsRealKey(t *testing.T) {
	r := prowlarrWired(newRoutes(t), "")
	env, _ := prowlarrEnv(t, r, strings.Replace(prowlarrYML, "http://radarr:7878", "http://films:7878", 1), nil)
	rememberAppliedKeys(t, env, "sonarr-key", "radarr-key")

	require.NoError(t, Prowlarr(context.Background(), env))

	radarr := r.sentBody("PUT", "/api/v1/applications/2?forceSave=true")
	assert.Equal(t, "http://films:7878", valueOf(radarr, "baseUrl"))
	assert.Equal(t, "radarr-key", valueOf(radarr, "apiKey"))
}

const (
	tvIndexer     = `{"id": 1, "name": "TV and more", "enable": true, "capabilities": {"categories": [{"id": 3000, "subCategories": [{"id": 5040}]}]}}`
	moviesIndexer = `{"id": 2, "name": "YTS", "enable": true, "capabilities": {"categories": [{"id": 2000, "subCategories": [{"id": 2040}]}]}}`
	syncYML       = "applications:\n- name: Sonarr\n  url: http://sonarr:8989\n  api_key: SONARR_API_KEY\n"
)

func syncEnv(t *testing.T, indexers string, inSonarr ...string) (*routes, Env, *said) {
	t.Helper()
	listed := make([]string, len(inSonarr))
	for n, name := range inSonarr {
		listed[n] = jsonOf(t, map[string]any{"id": n + 1, "name": name})
	}
	r := newRoutes(t).
		on("GET", "http://prowlarr/api/v1/indexer", ok(indexers)).
		on("GET", "http://prowlarr/api/v1/applications", ok("["+wiredSonarr+"]")).
		on("POST", "http://prowlarr/api/v1/command").
		on("GET", "http://sonarr-here/api/v3/indexer", ok("["+strings.Join(listed, ",")+"]"))
	env, out := prowlarrEnv(t, r, syncYML, nil)
	env.Settings["SONARR_URL"] = "http://sonarr-here"
	return r, env, out
}

func TestAnAppMissingIndexersProwlarrWouldSendItIsSyncedAgain(t *testing.T) {
	r, env, out := syncEnv(t, "["+tvIndexer+"]")

	require.NoError(t, ProwlarrSync(context.Background(), env))

	assert.Equal(t, []string{"POST /api/v1/command"}, r.writes("prowlarr"))
	assert.Equal(t, map[string]any{"name": "ApplicationIndexerSync"}, r.sentBody("POST", "/api/v1/command"))
	assert.Contains(t, out.lines, "prowlarr: sync indexers again: Sonarr has 0 of 1")
	assert.Contains(t, out.lines, "prowlarr: Sonarr still has 0 of prowlarr's 1 indexers; prowlarr will retry on its own schedule")
}

func TestTheAppsIndexersAreCountedWithTheAppsOwnKeyAtItsAddressOnThisMachine(t *testing.T) {
	r, env, _ := syncEnv(t, "["+tvIndexer+"]", "TV and more (Prowlarr)")

	require.NoError(t, ProwlarrSync(context.Background(), env))

	sonarr := r.to([]string{"sonarr-here"})
	require.Len(t, sonarr, 1)
	assert.Equal(t, "sonarr-key", sonarr[0].headers.Get("X-Api-Key"))
}

func TestAppsWithEveryIndexerProwlarrSendsThemAreLeftAlone(t *testing.T) {
	r, env, out := syncEnv(t, "["+tvIndexer+","+moviesIndexer+"]", "TV and more (Prowlarr)", "Added by hand")

	require.NoError(t, ProwlarrSync(context.Background(), env))

	assert.Empty(t, r.writes())
	assert.Empty(t, out.lines)
}

func TestADisabledIndexerIsNotExpectedInAnyApp(t *testing.T) {
	r, env, _ := syncEnv(t, "["+strings.Replace(tvIndexer, `"enable": true`, `"enable": false`, 1)+"]")

	require.NoError(t, ProwlarrSync(context.Background(), env))

	assert.Empty(t, r.writes())
}

func TestIndexersThatArriveWhileWaitingEndTheWaitWithoutANote(t *testing.T) {
	r, env, out := syncEnv(t, "["+tvIndexer+"]")
	r.on("GET", "http://sonarr-here/api/v3/indexer", ok("[]"), ok(`[{"id": 1, "name": "TV and more (Prowlarr)"}]`))

	require.NoError(t, ProwlarrSync(context.Background(), env))

	for _, line := range out.lines {
		assert.NotContains(t, line, "still has")
	}
}

func TestAnAppsAddressOnThisMachineKeepsTheDeclaredPortAndPath(t *testing.T) {
	for declared, address := range map[string]string{
		"http://sonarr:8989":        "http://localhost:8989",
		"http://sonarr":             "http://localhost",
		"http://sonarr:8989/sonarr": "http://localhost:8989/sonarr",
	} {
		t.Run(declared, func(t *testing.T) {
			env, _ := testEnv(t, nil, nil)

			assert.Equal(t, address, addressFromThisMachine(env, "Sonarr", declared))
		})
	}
}

func TestAMissingProwlarrYMLFailsTheStep(t *testing.T) {
	r := newRoutes(t)
	env, _ := testEnv(t, r.client(), prowlarrSecrets)

	err := Prowlarr(context.Background(), env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "prowlarr.yml")
	assert.Empty(t, r.requests)
}
