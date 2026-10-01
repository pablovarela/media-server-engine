load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/maintainerr"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets" "$DATA_DIR/volumes/.wiring" "$DATA_DIR/volumes/seerr/config"
  printf 'SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n' > "$ENGINE_DIR/.secrets/apps.env"
  echo jellyfin-key > "$DATA_DIR/volumes/.wiring/jellyfin.key"
  printf '{"main": {"apiKey": "seerr-key"}}' > "$DATA_DIR/volumes/seerr/config/settings.json"
}

teardown() {
  stop_fake_app
  teardown_stubs
}

wire() {
  MAINTAINERR_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/70-maintainerr.sh"
}

body_of() {
  python3 -c '
import json, sys
for line in open(sys.argv[1]):
    write = json.loads(line)
    if write["method"] + " " + write["path"] == sys.argv[2]:
        print(json.dumps(write["body"], sort_keys=True))' "$FAKE_APP_WRITES" "$1"
}

@test "a fresh maintainerr is connected to jellyfin, seerr, sonarr and radarr" {
  start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "$(printf '%s\n' \
    'POST /api/settings/jellyfin' 'POST /api/settings/seerr' 'POST /api/settings/sonarr' 'POST /api/settings/radarr')" ]
  [ "$(body_of 'POST /api/settings/jellyfin')" = '{"jellyfin_api_key": "jellyfin-key", "jellyfin_url": "http://jellyfin:8096"}' ]
  [ "$(body_of 'POST /api/settings/seerr')" = '{"api_key": "seerr-key", "url": "http://seerr:5055"}' ]
  [ "$(body_of 'POST /api/settings/sonarr')" = '{"apiKey": "sonarr-key", "serverName": "Sonarr", "url": "http://sonarr:8989"}' ]
  echo "$output" | grep -q "maintainerr: connect jellyfin"
  ! echo "$output" | grep -q "sonarr-key" || false
}

@test "an already wired maintainerr is left untouched" {
  start_fake_app "$FIXTURES/wired.json"
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  [ -z "$output" ]
}

@test "a rotated radarr key is corrected with exactly one write" {
  start_fake_app "$FIXTURES/wired.json"
  sed -i.bak 's/^RADARR_API_KEY=.*/RADARR_API_KEY=rotated/' "$ENGINE_DIR/.secrets/apps.env"
  run wire
  [ "$(fake_app_writes)" = "PUT /api/settings/radarr/1" ]
  [ "$(body_of 'PUT /api/settings/radarr/1')" = '{"apiKey": "rotated", "serverName": "Radarr", "url": "http://radarr:7878"}' ]
}

@test "a new jellyfin key is sent again" {
  start_fake_app "$FIXTURES/wired.json"
  echo newer-key > "$DATA_DIR/volumes/.wiring/jellyfin.key"
  run wire
  [ "$(fake_app_writes)" = "POST /api/settings/jellyfin" ]
}

@test "a dry run reports the changes and writes nothing" {
  start_fake_app "$FIXTURES/fresh.json"
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  echo "$output" | grep -q "(dry run) maintainerr: connect radarr"
}

@test "without the stored jellyfin key the other connections are still made" {
  start_fake_app "$FIXTURES/fresh.json"
  rm "$DATA_DIR/volumes/.wiring/jellyfin.key"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "jellyfin.key"
  fake_app_writes | grep -q "POST /api/settings/sonarr"
}

@test "a setting maintainerr answers as not ok fails the step" {
  start_fake_app "$FIXTURES/fresh.json"
  python3 -c '
import json, sys
state = json.load(open(sys.argv[1]))
state["effects"]["POST /api/settings/seerr"] = {"respond": {"status": "NOK", "code": 0, "message": "Seerr did not answer"}}
json.dump(state, open(sys.argv[1], "w"))' "$FAKE_APP_STATE"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "Seerr did not answer"
}

@test "a dry run before jellyfin's key exists still checks the other connections" {
  start_fake_app "$FIXTURES/fresh.json"
  rm "$DATA_DIR/volumes/.wiring/jellyfin.key"
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "once the jellyfin wiring has stored its key"
  echo "$output" | grep -q "(dry run) maintainerr: connect sonarr"
  [ -z "$(fake_app_writes)" ]
}
