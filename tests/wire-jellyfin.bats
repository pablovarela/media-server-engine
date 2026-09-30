load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/jellyfin"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets"
  printf 'JELLYFIN_ADMIN_PASSWORD=admin pass\n' > "$ENGINE_DIR/.secrets/apps.env"
  cp "$BATS_TEST_DIRNAME/../config-template/apps.yml" "$CONFIG_DIR/apps.yml"
  export JELLYFIN_ADMIN_USER=admin
}

teardown() {
  stop_fake_app
  teardown_stubs
}

wire() {
  JELLYFIN_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/20-jellyfin.sh"
}

store_key() {
  mkdir -p "$DATA_DIR/volumes/.wiring"
  echo "$1" > "$DATA_DIR/volumes/.wiring/jellyfin.key"
}

body_of() {
  python3 -c '
import json, sys
for line in open(sys.argv[1]):
    write = json.loads(line)
    if write["path"].startswith(sys.argv[2]):
        print(json.dumps(write["body"]))' "$FAKE_APP_WRITES" "$1"
}

edit_state() {
  python3 -c "
import json, sys
state = json.load(open(sys.argv[1]))
$1
json.dump(state, open(sys.argv[1], 'w'))" "$FAKE_APP_STATE"
}

@test "a fresh jellyfin completes the wizard, then gets libraries and an api key" {
  start_fake_app "$FIXTURES/fresh.json"
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "$(printf '%s\n' \
    'POST /Startup/Configuration' \
    'POST /Startup/User' \
    'POST /Startup/RemoteAccess' \
    'POST /Startup/Complete' \
    'POST /Users/AuthenticateByName' \
    'POST /Auth/Keys?app=media-server' \
    'POST /System/Configuration' \
    'POST /Library/VirtualFolders?name=Shows&collectionType=tvshows&paths=%2Fdata%2Ftvshows&refreshLibrary=false' \
    'POST /Library/VirtualFolders?name=Movies&collectionType=movies&paths=%2Fdata%2Fmovies&refreshLibrary=false')" ]
  [ "$(body_of /Startup/User)" = '{"Name": "admin", "Password": "admin pass"}' ]
  [ "$(body_of /Startup/Configuration | python3 -c 'import json,sys; print(json.load(sys.stdin)["ServerName"])')" = Media ]
  [ "$(cat "$DATA_DIR/volumes/.wiring/jellyfin.key")" = new-key ]
  [ "$(stat -f %Lp "$DATA_DIR/volumes/.wiring/jellyfin.key" 2>/dev/null || stat -c %a "$DATA_DIR/volumes/.wiring/jellyfin.key")" = 600 ]
  echo "$output" | grep -q "jellyfin: add library Shows"
}

@test "a completed wizard is not run again" {
  start_fake_app "$FIXTURES/wired.json"
  run wire
  ! fake_app_writes | grep -q "/Startup/" || false
}

@test "an already wired jellyfin is left untouched" {
  start_fake_app "$FIXTURES/wired.json"
  store_key stored-key
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  [ -z "$output" ]
}

@test "a drifted server name is corrected with one write that keeps the other settings" {
  start_fake_app "$FIXTURES/wired.json"
  store_key stored-key
  edit_state 'state["/System/Configuration"]["ServerName"] = "Other"'
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "POST /System/Configuration" ]
  [ "$(body_of /System/Configuration)" = '{"ServerName": "Media", "EnableMetrics": false, "UICulture": "en-US"}' ]
  echo "$output" | grep -q "jellyfin: set server name Other -> Media"
}

@test "a declared path missing from a library is added to it" {
  start_fake_app "$FIXTURES/wired.json"
  store_key stored-key
  edit_state 'state["/Library/VirtualFolders"][2]["Locations"] = []'
  run wire
  [ "$(fake_app_writes)" = "POST /Library/VirtualFolders/Paths?refreshLibrary=false" ]
  [ "$(body_of /Library/VirtualFolders/Paths)" = '{"Name": "Shows", "PathInfo": {"Path": "/data/tvshows"}}' ]
}

@test "a library of another type fails the step and the rest is still wired" {
  start_fake_app "$FIXTURES/wired.json"
  store_key stored-key
  edit_state 'state["/Library/VirtualFolders"][2]["CollectionType"] = "movies"; state["/System/Configuration"]["ServerName"] = "Other"'
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "library Shows is movies, not tvshows"
  fake_app_writes | grep -q "POST /System/Configuration"
}

@test "a key that exists in jellyfin is stored without creating another" {
  start_fake_app "$FIXTURES/wired.json"
  run wire
  [ "$status" -eq 0 ]
  ! fake_app_writes | grep -q "/Auth/Keys" || false
  [ "$(cat "$DATA_DIR/volumes/.wiring/jellyfin.key")" = stored-key ]
}

@test "a dry run on a fresh jellyfin reports the wizard and writes nothing" {
  start_fake_app "$FIXTURES/fresh.json"
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  echo "$output" | grep -q "(dry run) jellyfin: complete the startup wizard with admin user admin"
  [ ! -e "$DATA_DIR/volumes/.wiring/jellyfin.key" ]
}

@test "undeclared libraries are never deleted" {
  start_fake_app "$FIXTURES/wired.json"
  store_key stored-key
  run wire
  ! fake_app_writes | grep -q "^DELETE" || false
}

@test "a sign-in jellyfin refuses fails with the admin user named" {
  FAKE_APP_REJECT="admin pass" start_fake_app "$FIXTURES/wired.json"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "sign in as admin"
}
