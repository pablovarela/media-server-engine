load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/bazarr"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets" "$DATA_DIR/volumes/bazarr/config/config"
  printf 'SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n' > "$ENGINE_DIR/.secrets/apps.env"
  printf 'auth:\n  apikey: bazarr-key\n' > "$DATA_DIR/volumes/bazarr/config/config/config.yaml"
  printf 'bazarr:\n  languages: [en]\n' > "$CONFIG_DIR/apps.yml"
  export FAKE_APP_HEADERS='{"X-API-KEY": "bazarr-key"}'
}

teardown() {
  stop_fake_app
  teardown_stubs
}

wire() {
  BAZARR_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/60-bazarr.sh"
}

field() {
  python3 -c '
import json, sys
body = json.loads(open(sys.argv[1]).readline())["body"]
print(json.dumps(body.get(sys.argv[2])))' "$FAKE_APP_WRITES" "$1"
}

profiles_posted() {
  python3 -c '
import json, sys
body = json.loads(open(sys.argv[1]).readline())["body"]
for profile in json.loads(body["languages-profiles"]):
    print(profile["profileId"], profile["name"], ",".join(i["language"] for i in profile["items"]), profile.get("tag"))' "$FAKE_APP_WRITES"
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
  [ "$(field settings-sonarr-ip)" = '"sonarr"' ]
  [ "$(field settings-radarr-apikey)" = '"radarr-key"' ]
  [ "$(field settings-general-use_sonarr)" = '"true"' ]
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

@test "a fresh bazarr gets the declared languages in a default profile for series and movies" {
  start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "POST /api/system/settings" ]
  [ "$(field languages-enabled)" = '"en"' ]
  [ "$(profiles_posted)" = "1 Default en None" ]
  [ "$(field settings-general-serie_default_enabled)" = '"true"' ]
  [ "$(field settings-general-serie_default_profile)" = '"1"' ]
  [ "$(field settings-general-movie_default_profile)" = '"1"' ]
  echo "$output" | grep -q "bazarr: create subtitle profile Default with en"
}

@test "an added language joins the default profile, which keeps its name and tag" {
  start_fake_app "$FIXTURES/wired.json"
  printf 'bazarr:\n  languages: [en, es]\n' > "$CONFIG_DIR/apps.yml"
  run wire
  [ "$(field languages-enabled)" = '["en", "es"]' ]
  [ "$(profiles_posted)" = "1 English en,es english" ]
  [ "$(field settings-general-serie_default_profile)" = null ]
  echo "$output" | grep -q "bazarr: set subtitle profile English languages en -> en, es"
}

@test "other subtitle profiles are sent back unchanged, since bazarr deletes any it is not sent" {
  start_fake_app "$FIXTURES/wired.json"
  python3 -c '
import copy, json, sys
state = json.load(open(sys.argv[1]))
other = copy.deepcopy(state["/api/system/languages/profiles"][0])
other.update(profileId=2, name="Other", tag=None)
state["/api/system/languages/profiles"].append(other)
json.dump(state, open(sys.argv[1], "w"))' "$FAKE_APP_STATE"
  printf 'bazarr:\n  languages: [en, fr]\n' > "$CONFIG_DIR/apps.yml"
  run wire
  [ "$(profiles_posted)" = "$(printf '1 English en,fr english\n2 Other en None')" ]
}

@test "a language bazarr does not know fails the step" {
  start_fake_app "$FIXTURES/wired.json"
  printf 'bazarr:\n  languages: [en, xx]\n' > "$CONFIG_DIR/apps.yml"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "bazarr has no language xx"
}

@test "without declared languages the subtitle profiles are left alone" {
  start_fake_app "$FIXTURES/fresh.json"
  : > "$CONFIG_DIR/apps.yml"
  run wire
  [ "$(field languages-profiles)" = null ]
}
