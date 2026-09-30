load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/seerr"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets" "$DATA_DIR/volumes/seerr/config"
  printf 'SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\nJELLYFIN_ADMIN_PASSWORD=admin pass\n' > "$ENGINE_DIR/.secrets/apps.env"
  printf '{"main": {"apiKey": "seerr-key"}}' > "$DATA_DIR/volumes/seerr/config/settings.json"
  cp "$BATS_TEST_DIRNAME/../config-template/apps.yml" "$CONFIG_DIR/apps.yml"
  export JELLYFIN_ADMIN_USER=admin FAKE_APP_HEADERS='{"X-Api-Key": "seerr-key"}'
}

teardown() {
  stop_fake_app
  teardown_stubs
}

wire() {
  SEERR_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/50-seerr.sh"
}

body_of() {
  python3 -c '
import json, sys
for line in open(sys.argv[1]):
    write = json.loads(line)
    if write["method"] + " " + write["path"] == sys.argv[2]:
        print(json.dumps(write["body"], sort_keys=True))' "$FAKE_APP_WRITES" "$1"
}

value_in() {
  python3 -c 'import json, sys; print(json.dumps(json.load(sys.stdin)[sys.argv[1]]))' "$1"
}

edit_state() {
  python3 -c "
import json, sys
state = json.load(open(sys.argv[1]))
$1
json.dump(state, open(sys.argv[1], 'w'))" "$FAKE_APP_STATE"
}

@test "a fresh seerr signs in with jellyfin, gets libraries, sonarr and radarr, then is initialised" {
  start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "$(printf '%s\n' \
    'POST /api/v1/auth/jellyfin' \
    'POST /api/v1/settings/jellyfin/library/sync' \
    'PUT /api/v1/settings/jellyfin/library/s1' \
    'PUT /api/v1/settings/jellyfin/library/m1' \
    'POST /api/v1/settings/sonarr/test' \
    'POST /api/v1/settings/sonarr' \
    'POST /api/v1/settings/radarr/test' \
    'POST /api/v1/settings/radarr' \
    'POST /api/v1/settings/initialize')" ]
  auth=$(body_of 'POST /api/v1/auth/jellyfin')
  [ "$(echo "$auth" | value_in username)" = '"admin"' ]
  [ "$(echo "$auth" | value_in password)" = '"admin pass"' ]
  [ "$(echo "$auth" | value_in hostname)" = '"jellyfin"' ]
  [ "$(echo "$auth" | value_in serverType)" = 2 ]
  sonarr=$(body_of 'POST /api/v1/settings/sonarr')
  [ "$(echo "$sonarr" | value_in activeProfileId)" = 7 ]
  [ "$(echo "$sonarr" | value_in activeDirectory)" = '"/tv"' ]
  [ "$(echo "$sonarr" | value_in apiKey)" = '"sonarr-key"' ]
  [ "$(echo "$sonarr" | value_in isDefault)" = true ]
  [ "$(body_of 'POST /api/v1/settings/radarr' | value_in minimumAvailability)" = '"released"' ]
  [ "$(body_of 'PUT /api/v1/settings/jellyfin/library/s1')" = '{"enabled": true}' ]
}

@test "an already wired seerr is left untouched" {
  start_fake_app "$FIXTURES/wired.json"
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  [ -z "$output" ]
}

@test "a rotated sonarr key is corrected with exactly one write" {
  start_fake_app "$FIXTURES/wired.json"
  sed -i.bak 's/^SONARR_API_KEY=.*/SONARR_API_KEY=rotated/' "$ENGINE_DIR/.secrets/apps.env"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "PUT /api/v1/settings/sonarr/0" ]
  [ "$(body_of 'PUT /api/v1/settings/sonarr/0' | value_in apiKey)" = '"rotated"' ]
  [ "$(body_of 'PUT /api/v1/settings/sonarr/0' | value_in enableSeasonFolders)" = false ]
  echo "$output" | grep -q "seerr: set sonarr api key"
}

@test "a changed quality profile is looked up by name" {
  start_fake_app "$FIXTURES/wired.json"
  edit_state 'state["/api/v1/settings/sonarr"][0].update(activeProfileId=4, activeProfileName="Any")'
  run wire
  [ "$(fake_app_writes)" = "$(printf '%s\n' 'POST /api/v1/settings/sonarr/test' 'PUT /api/v1/settings/sonarr/0')" ]
  [ "$(body_of 'PUT /api/v1/settings/sonarr/0' | value_in activeProfileId)" = 7 ]
  echo "$output" | grep -q "seerr: set sonarr quality profile Any -> WEB-1080p"
}

@test "a declared library that is not enabled is enabled without a sync" {
  start_fake_app "$FIXTURES/wired.json"
  edit_state 'state["/api/v1/settings/jellyfin/library"][1]["enabled"] = False'
  run wire
  [ "$(fake_app_writes)" = "PUT /api/v1/settings/jellyfin/library/s1" ]
}

@test "a quality profile the app does not have fails the step and the other app is still wired" {
  start_fake_app "$FIXTURES/fresh.json"
  sed -i.bak 's/quality_profile: WEB-1080p/quality_profile: Missing/' "$CONFIG_DIR/apps.yml"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "sonarr has no quality profile Missing"
  fake_app_writes | grep -q "POST /api/v1/settings/radarr$"
}

@test "a declared library jellyfin does not have fails the step" {
  start_fake_app "$FIXTURES/fresh.json"
  sed -i.bak 's/libraries: \[Shows, Movies\]/libraries: [Shows, Cartoons]/' "$CONFIG_DIR/apps.yml"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "jellyfin has no library Cartoons"
}

@test "a dry run on a fresh seerr reports the changes and writes nothing" {
  start_fake_app "$FIXTURES/fresh.json"
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  echo "$output" | grep -q "(dry run) seerr: sign in with jellyfin as admin"
  echo "$output" | grep -q "(dry run) seerr: initialise"
}

@test "a seerr that has not written its settings yet fails with a hint" {
  start_fake_app "$FIXTURES/fresh.json"
  rm "$DATA_DIR/volumes/seerr/config/settings.json"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "settings.json"
}

@test "a declared jellyfin external url is kept with one write" {
  start_fake_app "$FIXTURES/wired.json"
  python3 -c '
import sys, yaml
path = sys.argv[1]
config = yaml.safe_load(open(path))
config["seerr"]["jellyfin_external_url"] = "http://media.example:8096"
config.pop("jellyfin_external_url", None)
yaml.safe_dump(config, open(path, "w"))' "$CONFIG_DIR/apps.yml"
  run wire
  [ "$(fake_app_writes)" = "POST /api/v1/settings/jellyfin" ]
  [ "$(body_of 'POST /api/v1/settings/jellyfin' | value_in externalHostname)" = '"http://media.example:8096"' ]
  [ "$(body_of 'POST /api/v1/settings/jellyfin' | value_in ip)" = '"jellyfin"' ]
}
