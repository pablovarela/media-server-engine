load helpers

FIXTURES="$BATS_TEST_DIRNAME/fixtures/prowlarr"

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets"
  printf 'SONARR_API_KEY=sonarr-key\nPROWLARR_API_KEY=prowlarr-key\n' > "$ENGINE_DIR/.secrets/apps.env"
  printf 'applications:\n- name: Sonarr\n  url: http://sonarr:8989\n  api_key: SONARR_API_KEY\n' > "$CONFIG_DIR/prowlarr.yml"
  FAKE_APP_HEADERS='{"X-Api-Key": "prowlarr-key"}' start_fake_app "$FIXTURES/wired.json"
}

teardown() {
  stop_fake_app
  stop_second_fake_app
  teardown_stubs
}

sonarr_with_indexers() {
  python3 -c 'import json, sys; print(json.dumps({"/api/v3/indexer": [{"id": i + 1, "name": n} for i, n in enumerate(sys.argv[1:])]}))' "$@" > "$STUB_DIR/sonarr.json"
  FAKE_APP_HEADERS='{"X-Api-Key": "sonarr-key"}' start_second_fake_app "$STUB_DIR/sonarr.json"
}

sync() {
  PROWLARR_URL=$FAKE_APP_URL SONARR_URL=$SECOND_APP_URL WIRE_SYNC_WAIT_SECONDS=0 \
    "$BATS_TEST_DIRNAME/../scripts/wire/80-prowlarr-sync.sh"
}

@test "an app missing prowlarr's indexers gets them synced again" {
  sonarr_with_indexers
  run sync
  [ "$status" -eq 0 ]
  [ "$(fake_app_writes)" = "POST /api/v1/command" ]
  grep -q '"ApplicationIndexerSync"' "$FAKE_APP_WRITES"
  echo "$output" | grep -q "prowlarr: sync indexers again: Sonarr has 0 of 1"
}

@test "an app still missing them after the wait is noted, without failing the wiring" {
  sonarr_with_indexers
  run sync
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "Sonarr still has 0 of prowlarr's 1 indexers; prowlarr will retry on its own schedule"
}

@test "apps that have every indexer are left alone" {
  sonarr_with_indexers "The Pirate Bay (Prowlarr)" "Added by hand"
  run sync
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  [ -z "$output" ]
}

@test "a dry run reports the sync and asks for nothing" {
  sonarr_with_indexers
  WIRE_DRY_RUN=1 run sync
  [ "$status" -eq 0 ]
  [ -z "$(fake_app_writes)" ]
  echo "$output" | grep -q "prowlarr: sync indexers again: Sonarr has 0 of 1"
}
