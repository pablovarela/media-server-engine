load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/library-updates"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets" "$DATA_DIR/volumes/.wiring"
  printf 'SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n' > "$ENGINE_DIR/.secrets/apps.env"
  echo jellyfin-key > "$DATA_DIR/volumes/.wiring/jellyfin.key"
}

teardown() {
  stop_fake_app
  stop_second_fake_app
  teardown_stubs
}

start_arrs() {
  FAKE_APP_HEADERS='{"X-Api-Key": "sonarr-key"}' start_fake_app "$FIXTURES/sonarr-$1.json"
  FAKE_APP_HEADERS='{"X-Api-Key": "radarr-key"}' start_second_fake_app "$FIXTURES/radarr-$1.json"
}

wire() {
  SONARR_URL=$FAKE_APP_URL RADARR_URL=$SECOND_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/25-library-updates.sh"
}

remember_applied_key() {
  for app in sonarr radarr; do
    python3 -c 'import hashlib, sys; print(hashlib.sha256(sys.argv[1].encode()).hexdigest())' "$1" > "$DATA_DIR/volumes/.wiring/$app-jellyfin-connection.sha256"
  done
}

body() {
  python3 -c '
import json, sys
write = json.loads(open(sys.argv[1]).readline())
body = write["body"]
fields = {f["name"]: f.get("value") for f in body["fields"]}
events = sorted(k for k, v in body.items() if k.startswith("on") and v is True)
print(body["name"], fields["host"], fields["port"], fields["apiKey"], fields["updateLibrary"], fields["notify"], " ".join(events))' "$1"
}

@test "fresh sonarr and radarr tell jellyfin about every library change" {
  start_arrs fresh
  run wire
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "POST /api/v3/notification?forceSave=true" ]
  [ "$(body "$FAKE_APP_WRITES")" = "Emby / Jellyfin jellyfin 8096 jellyfin-key True False onApplicationUpdate onDownload onEpisodeFileDelete onEpisodeFileDeleteForUpgrade onGrab onImportComplete onRename onSeriesAdd onSeriesDelete onUpgrade" ]
  body "$SECOND_APP_WRITES" | grep -q "^Emby / Jellyfin jellyfin 8096 jellyfin-key True False .*onMovieDelete"
  echo "$output" | grep -q "sonarr: add the Jellyfin connection"
}

@test "already connected arrs are left untouched" {
  start_arrs wired
  remember_applied_key jellyfin-key
  run wire
  [ "$status" -eq 0 ]
  [ -z "$(cat "$FAKE_APP_WRITES" "$SECOND_APP_WRITES")" ]
  [ -z "$output" ]
}

@test "a new jellyfin key reaches both connections once" {
  start_arrs wired
  remember_applied_key old-key
  run wire
  [ "$(fake_app_writes)" = "PUT /api/v3/notification/2?forceSave=true" ]
  body "$FAKE_APP_WRITES" | grep -q " jellyfin-key "
  : > "$FAKE_APP_WRITES"; : > "$SECOND_APP_WRITES"
  run wire
  [ -z "$(cat "$FAKE_APP_WRITES" "$SECOND_APP_WRITES")" ]
}

@test "a connection pointed elsewhere is corrected, keeping its events" {
  start_arrs wired
  remember_applied_key jellyfin-key
  python3 -c '
import json, sys
state = json.load(open(sys.argv[1]))
item = state["/api/v3/notification"][0]
item["onGrab"] = False
next(f for f in item["fields"] if f["name"] == "host")["value"] = "old-host"
json.dump(state, open(sys.argv[1], "w"))' "$FAKE_APP_STATE"
  run wire
  [ "$(fake_app_writes)" = "PUT /api/v3/notification/2?forceSave=true" ]
  body "$FAKE_APP_WRITES" | grep -q "^Emby / Jellyfin jellyfin 8096"
  ! body "$FAKE_APP_WRITES" | grep -q "onGrab" || false
  echo "$output" | grep -q "sonarr: set the Jellyfin connection host old-host -> jellyfin"
}

@test "a dry run reports and writes nothing" {
  start_arrs fresh
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  [ -z "$(cat "$FAKE_APP_WRITES" "$SECOND_APP_WRITES")" ]
  echo "$output" | grep -q "(dry run) radarr: add the Jellyfin connection"
}

@test "without the stored jellyfin key the step fails with a hint" {
  start_arrs fresh
  rm "$DATA_DIR/volumes/.wiring/jellyfin.key"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "jellyfin.key"
}
