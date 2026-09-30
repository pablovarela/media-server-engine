load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/prowlarr"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets"
  printf 'SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\nPROWLARR_API_KEY=prowlarr-key\n' > "$ENGINE_DIR/.secrets/apps.env"
  cp "$FIXTURES/prowlarr.yml" "$CONFIG_DIR/prowlarr.yml"
  export FAKE_APP_HEADERS='{"X-Api-Key": "prowlarr-key"}'
}

teardown() {
  stop_fake_app
  teardown_stubs
}

wire() {
  PROWLARR_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/10-prowlarr.sh"
}

remember_applied_keys() {
  mkdir -p "$DATA_DIR/volumes/.wiring"
  for app in Sonarr:sonarr-key Radarr:radarr-key; do
    python3 -c 'import hashlib, sys; print(hashlib.sha256(("%s\0%s" % (sys.argv[1], sys.argv[2])).encode()).hexdigest())' \
      "${app%%:*}" "${app#*:}" > "$DATA_DIR/volumes/.wiring/prowlarr-application-${app%%:*}.sha256"
  done
}

written() {
  python3 -c '
import json, sys
for line in open(sys.argv[1]):
    write = json.loads(line)
    if sys.argv[2] in write["path"]:
        print(json.dumps(write["body"]))' "$FAKE_APP_WRITES" "$1"
}

field_of() {
  python3 -c '
import json, sys
body = json.loads(sys.stdin.readline())
print(json.dumps(next(f.get("value") for f in body["fields"] if f["name"] == sys.argv[1])))' "$1"
}

@test "a fresh prowlarr gets the proxy, indexers and applications, then syncs" {
  start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "$(printf '%s\n' \
    'POST /api/v1/tag' \
    'POST /api/v1/indexerproxy?forceSave=true' \
    'POST /api/v1/indexer?forceSave=true' \
    'POST /api/v1/applications?forceSave=true' \
    'POST /api/v1/applications?forceSave=true' \
    'POST /api/v1/command')" ]
  [ "$(written /indexerproxy | field_of host)" = '"http://localhost:8191/"' ]
  [ "$(written /indexer? | python3 -c 'import json,sys; b=json.load(sys.stdin); print(b["priority"], b["tags"], b["appProfileId"])')" = "25 [1] 1" ]
  [ "$(written /indexer? | field_of apiurl)" = '"apibay.org"' ]
  [ "$(written /applications | head -1 | field_of apiKey)" = '"sonarr-key"' ]
  [ "$(written /applications | head -1 | field_of prowlarrUrl)" = '"http://gluetun:9696"' ]
  [ "$(written /applications | head -1 | field_of syncCategories)" = "[5000, 5010, 5020, 5030, 5040, 5045, 5050]" ]
  [ "$(written /applications | tail -1 | field_of baseUrl)" = '"http://radarr:7878"' ]
  echo "$output" | grep -q "prowlarr: add indexer The Pirate Bay"
}

@test "an already wired prowlarr is left untouched" {
  start_fake_app "$FIXTURES/wired.json"
  remember_applied_keys
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  [ -z "$output" ]
}

@test "a drifted priority is corrected with exactly one write, then synced" {
  start_fake_app "$FIXTURES/wired.json"
  remember_applied_keys
  python3 -c '
import json, sys
state = json.load(open(sys.argv[1]))
state["/api/v1/indexer"][0]["priority"] = 50
json.dump(state, open(sys.argv[1], "w"))' "$FAKE_APP_STATE"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "$(printf '%s\n' 'PUT /api/v1/indexer/1?forceSave=true' 'POST /api/v1/command')" ]
  echo "$output" | grep -q "prowlarr: set indexer The Pirate Bay priority 50 -> 25"
}

@test "application keys are applied once when no applied key is remembered" {
  start_fake_app "$FIXTURES/wired.json"
  run wire
  [ "$(fake_app_writes | grep -c '^PUT /api/v1/applications/')" -eq 2 ]
  [ "$(stat -f %Lp "$DATA_DIR/volumes/.wiring/prowlarr-application-Sonarr.sha256" 2>/dev/null || stat -c %a "$DATA_DIR/volumes/.wiring/prowlarr-application-Sonarr.sha256")" = 600 ]
  : > "$FAKE_APP_WRITES"
  run wire
  [ -z "$(fake_app_writes)" ]
}

@test "a rotated key reaches the application" {
  start_fake_app "$FIXTURES/wired.json"
  remember_applied_keys
  sed -i.bak 's/^SONARR_API_KEY=.*/SONARR_API_KEY=rotated/' "$ENGINE_DIR/.secrets/apps.env"
  run wire
  [ "$(fake_app_writes | grep -c '^PUT /api/v1/applications/')" -eq 1 ]
  [ "$(written /applications/ | field_of apiKey)" = '"rotated"' ]
}

@test "a dry run reports the changes and writes nothing" {
  start_fake_app "$FIXTURES/fresh.json"
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  echo "$output" | grep -q "(dry run) prowlarr: add application Sonarr"
  [ ! -e "$DATA_DIR/volumes/.wiring" ]
}

@test "indexers that are not declared are never deleted" {
  start_fake_app "$FIXTURES/wired.json"
  remember_applied_keys
  printf 'indexers: []\napplications: []\n' > "$CONFIG_DIR/prowlarr.yml"
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
}

@test "an item prowlarr rejects fails the step but the rest is still wired" {
  FAKE_APP_REJECT=thepiratebay start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "could not save The Pirate Bay"
  [ "$(fake_app_writes | grep -c '^POST /api/v1/applications')" -eq 2 ]
}

@test "a wrong prowlarr api key fails with the answer" {
  start_fake_app "$FIXTURES/fresh.json"
  sed -i.bak 's/^PROWLARR_API_KEY=.*/PROWLARR_API_KEY=wrong/' "$ENGINE_DIR/.secrets/apps.env"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "401"
}

@test "an indexer naming an undeclared proxy is refused" {
  start_fake_app "$FIXTURES/fresh.json"
  printf 'indexers:\n  - name: X\n    definition: 1337x\n    proxy: Nope\n' > "$CONFIG_DIR/prowlarr.yml"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "proxy Nope"
}

@test "an indexer prowlarr cannot reach is reported but does not fail the step" {
  FAKE_APP_REJECT=thepiratebay FAKE_APP_REJECT_MESSAGE="Unable to connect to indexer. Unexpected response status UnavailableForLegalReasons" start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "prowlarr: could not reach indexer The Pirate Bay"
  [ "$(fake_app_writes | grep -c '^POST /api/v1/applications')" -eq 2 ]
}

@test "the template declares indexers that prowlarr knows" {
  python3 -c '
import json, sys, yaml
template = yaml.safe_load(open(sys.argv[1]))
names = {i["name"] for i in template["indexers"]}
assert {"1337x", "The Pirate Bay", "YTS", "LimeTorrents"} <= names, names
assert all(i.get("proxy") in (None, "FlareSolverr") for i in template["indexers"])' "$BATS_TEST_DIRNAME/../config-template/prowlarr.yml"
}
