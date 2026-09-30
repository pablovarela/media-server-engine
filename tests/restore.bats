load helpers

setup() {
  setup_stubs
  make_stub docker 'if [ "$1" = ps ] && [ "$4" = "label=com.docker.compose.project=${FAKE_PROJECT:-$(basename "$MEDIA_SERVER_DIR" | tr "[:upper:]" "[:lower:]" | tr -cd "a-z0-9_-")}" ]; then printf "%s" "${FAKE_RUNNING:-}"; fi'
  make_stub restic ''
  make_stub git ''
}

teardown() {
  teardown_stubs
}

@test "restore refuses while the stack is running" {
  FAKE_RUNNING=abc123 run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "stack is running"
  ! grep -q "restic restore" "$STUB_LOG"
}

@test "restore refuses to overwrite existing app data" {
  mkdir -p "$MEDIA_SERVER_DIR/volumes/sonarr"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q -- "--overwrite"
  ! grep -q "restic restore" "$STUB_LOG"
}

@test "restore overwrites existing app data when asked" {
  mkdir -p "$MEDIA_SERVER_DIR/volumes/sonarr"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh" --overwrite
  [ "$status" -eq 0 ]
  grep -q "restic restore latest:/volumes --target $MEDIA_SERVER_DIR/volumes" "$STUB_LOG"
}

@test "restore into an empty checkout needs no flag" {
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  grep -q "restic restore latest:/volumes --target $MEDIA_SERVER_DIR/volumes" "$STUB_LOG"
}

@test "restore treats the tracked configarr config as no app data" {
  mkdir -p "$MEDIA_SERVER_DIR/volumes/configarr/config"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
}

@test "restore refuses when it cannot tell whether the stack is running" {
  make_stub docker 'exit 1'
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  ! grep -q "restic restore" "$STUB_LOG"
}

@test "restore leaves the git-tracked configarr config alone" {
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  grep -q "restic restore latest:/volumes .*--exclude configarr" "$STUB_LOG"
}

@test "restore --overwrite replaces app data instead of merging into it" {
  mkdir -p "$MEDIA_SERVER_DIR/volumes/sonarr/data"
  touch "$MEDIA_SERVER_DIR/volumes/sonarr/data/sonarr.db-wal"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh" --overwrite
  [ "$status" -eq 0 ]
  [ ! -e "$MEDIA_SERVER_DIR/volumes/sonarr/data/sonarr.db-wal" ]
  ls -d "$MEDIA_SERVER_DIR"/volumes.before-restore-*/sonarr/data/sonarr.db-wal
  grep -q "git checkout -- volumes/configarr" "$STUB_LOG"
}

@test "restore without --overwrite keeps volumes in place" {
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  ! ls -d "$MEDIA_SERVER_DIR"/volumes.before-restore-* 2>/dev/null
}

@test "restore finds the stack when the checkout name has capitals and dots" {
  mkdir -p "$MEDIA_SERVER_DIR/Media.Server"
  MEDIA_SERVER_DIR="$MEDIA_SERVER_DIR/Media.Server" FAKE_PROJECT=mediaserver FAKE_RUNNING=abc run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "stack is running"
}

@test "restore honours COMPOSE_PROJECT_NAME" {
  COMPOSE_PROJECT_NAME=custom FAKE_PROJECT=custom FAKE_RUNNING=abc run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "stack is running"
}
