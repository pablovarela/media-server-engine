load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/bazarr"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets" "$DATA_DIR/volumes/bazarr/config/config"
  printf 'SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n' > "$ENGINE_DIR/.secrets/apps.env"
  printf 'auth:\n  apikey: bazarr-key\n' > "$DATA_DIR/volumes/bazarr/config/config/config.yaml"
  export FAKE_APP_HEADERS='{"X-API-KEY": "bazarr-key"}'
}

teardown() {
  stop_fake_app
  teardown_stubs
}

wire() {
  BAZARR_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/60-bazarr.sh"
}

posted() {
  python3 -c '
import json, sys
for line in open(sys.argv[1]):
    print(json.dumps(json.loads(line)["body"], sort_keys=True))' "$FAKE_APP_WRITES"
}

@test "a fresh bazarr is connected to sonarr and radarr in one write" {
  start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "POST /api/system/settings" ]
  [ "$(posted)" = '{"settings-general-use_radarr": "true", "settings-general-use_sonarr": "true", "settings-radarr-apikey": "radarr-key", "settings-radarr-base_url": "", "settings-radarr-ip": "radarr", "settings-sonarr-apikey": "sonarr-key", "settings-sonarr-base_url": "", "settings-sonarr-ip": "sonarr"}' ]
  echo "$output" | grep -q "bazarr: set sonarr ip 127.0.0.1 -> sonarr"
  ! echo "$output" | grep -q "sonarr-key" || false
}

@test "an already wired bazarr is left untouched" {
  start_fake_app "$FIXTURES/wired.json"
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  [ -z "$output" ]
}

@test "a rotated radarr key is sent alone" {
  start_fake_app "$FIXTURES/wired.json"
  sed -i.bak 's/^RADARR_API_KEY=.*/RADARR_API_KEY=rotated/' "$ENGINE_DIR/.secrets/apps.env"
  run wire
  [ "$(posted)" = '{"settings-radarr-apikey": "rotated"}' ]
  echo "$output" | grep -q "bazarr: set radarr api key"
}

@test "a dry run reports the changes and writes nothing" {
  start_fake_app "$FIXTURES/fresh.json"
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  echo "$output" | grep -q "(dry run) bazarr: turn on use_sonarr"
}

@test "a bazarr without its config file fails with a hint" {
  start_fake_app "$FIXTURES/fresh.json"
  rm "$DATA_DIR/volumes/bazarr/config/config/config.yaml"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "config.yaml"
}

@test "a write bazarr rejects fails the step" {
  FAKE_APP_REJECT=sonarr-key start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -ne 0 ]
}
