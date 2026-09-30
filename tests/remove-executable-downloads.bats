load helpers

setup() {
  setup_stubs
  mkdir -p "$MEDIA_SERVER_DIR/volumes/sonarr/data" "$MEDIA_SERVER_DIR/volumes/radarr/config"
  echo "<Config><ApiKey>sonarr-key</ApiKey></Config>" > "$MEDIA_SERVER_DIR/volumes/sonarr/data/config.xml"
  echo "<Config><ApiKey>radarr-key</ApiKey></Config>" > "$MEDIA_SERVER_DIR/volumes/radarr/config/config.xml"
  make_stub curl '
case "$*" in
  *"-X DELETE"*) ;;
  *localhost:8989/api/v3/queue*) echo "{\"records\": [
      {\"id\": 11, \"title\": \"Show S01E01.exe\", \"statusMessages\": [{\"messages\": [\"Caution: Found executable file with extension: '"'"'.exe'"'"'\"]}]},
      {\"id\": 12, \"title\": \"Show S01E02\", \"statusMessages\": []}]}" ;;
  *localhost:7878/api/v3/queue*) if [ -n "${FAKE_RADARR_DOWN:-}" ]; then exit 7; fi; echo "{\"records\": []}" ;;
esac'
}

teardown() {
  teardown_stubs
}

@test "cleanup removes and blocklists downloads flagged as executables" {
  run "$BATS_TEST_DIRNAME/../scripts/remove-executable-downloads.sh"
  [ "$status" -eq 0 ]
  grep -q "curl .*-X DELETE .*localhost:8989/api/v3/queue/11?removeFromClient=true&blocklist=true&skipRedownload=false" "$STUB_LOG"
  echo "$output" | grep -q "Show S01E01.exe"
}

@test "cleanup leaves ordinary downloads alone" {
  run "$BATS_TEST_DIRNAME/../scripts/remove-executable-downloads.sh"
  ! grep -q "queue/12" "$STUB_LOG"
}

@test "cleanup checks radarr as well as sonarr" {
  run "$BATS_TEST_DIRNAME/../scripts/remove-executable-downloads.sh"
  grep -q "localhost:7878/api/v3/queue" "$STUB_LOG"
  grep -q "X-Api-Key: radarr-key" "$STUB_LOG"
}

@test "cleanup still checks sonarr when radarr is unreachable" {
  FAKE_RADARR_DOWN=1 run "$BATS_TEST_DIRNAME/../scripts/remove-executable-downloads.sh"
  grep -q "queue/11?removeFromClient" "$STUB_LOG"
  echo "$output" | grep -qi "radarr"
}

@test "cleanup removes a flagged season pack once, not once per episode" {
  make_stub curl '
case "$*" in
  *"-X DELETE"*) ;;
  *localhost:8989/api/v3/queue*) echo "{\"totalRecords\": 2, \"records\": [
      {\"id\": 21, \"downloadId\": \"PACK\", \"title\": \"Show S02.exe\", \"statusMessages\": [{\"messages\": [\"Found executable file\"]}]},
      {\"id\": 22, \"downloadId\": \"PACK\", \"title\": \"Show S02.exe\", \"statusMessages\": [{\"messages\": [\"Found executable file\"]}]}]}" ;;
  *) echo "{\"records\": []}" ;;
esac'
  run "$BATS_TEST_DIRNAME/../scripts/remove-executable-downloads.sh"
  [ "$status" -eq 0 ]
  [ "$(grep -c "X DELETE" "$STUB_LOG")" -eq 1 ]
}

@test "cleanup reads every page of the queue" {
  make_stub curl '
case "$*" in
  *"-X DELETE"*) ;;
  *localhost:8989/api/v3/queue*page=1*) echo "{\"totalRecords\": 2, \"records\": [{\"id\": 31, \"downloadId\": \"A\", \"title\": \"One.exe\", \"statusMessages\": [{\"messages\": [\"Found executable file\"]}]}]}" ;;
  *localhost:8989/api/v3/queue*page=2*) echo "{\"totalRecords\": 2, \"records\": [{\"id\": 32, \"downloadId\": \"B\", \"title\": \"Two.exe\", \"statusMessages\": [{\"messages\": [\"Found executable file\"]}]}]}" ;;
  *) echo "{\"records\": []}" ;;
esac'
  QUEUE_PAGE_SIZE=1 run "$BATS_TEST_DIRNAME/../scripts/remove-executable-downloads.sh"
  grep -q "queue/31?removeFromClient" "$STUB_LOG"
  grep -q "queue/32?removeFromClient" "$STUB_LOG"
}
