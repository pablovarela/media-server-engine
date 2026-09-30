load helpers

setup() {
  setup_stubs
  make_stub docker 'if [ "$1" = ps ] && [ "$4" = "label=com.docker.compose.project=media-server" ]; then printf "%s" "${FAKE_RUNNING:-}"; fi'
  make_stub restic 'if [ "$1" = snapshots ]; then if [ -n "${FAKE_OWN_SNAPSHOTS:-}" ]; then echo "[{\"id\":\"x\"}]"; else echo "[]"; fi; fi'
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
}

teardown() {
  teardown_stubs
}

@test "restore refuses while the stack is running" {
  FAKE_RUNNING=abc123 run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "stack is running"
  ! grep -q "restic restore" "$STUB_LOG" || false
}

@test "restore refuses when it cannot tell whether the stack is running" {
  make_stub docker 'exit 1'
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  ! grep -q "restic restore" "$STUB_LOG" || false
}

@test "restore refuses to overwrite existing app data" {
  mkdir -p "$DATA_DIR/volumes/sonarr"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q -- "--overwrite"
  ! grep -q "restic restore" "$STUB_LOG" || false
}

@test "restore into an empty data directory needs no flag" {
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  grep -q "restic restore latest:/volumes --target $DATA_DIR/volumes --exclude configarr" "$STUB_LOG"
}

@test "restore treats configarr's cache as no app data" {
  mkdir -p "$DATA_DIR/volumes/configarr/repos"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
}

@test "restore --overwrite moves existing app data aside instead of merging into it" {
  mkdir -p "$DATA_DIR/volumes/sonarr/data"
  touch "$DATA_DIR/volumes/sonarr/data/sonarr.db-wal"
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh" --overwrite
  [ "$status" -eq 0 ]
  [ ! -e "$DATA_DIR/volumes/sonarr/data/sonarr.db-wal" ]
  ls -d "$DATA_DIR"/volumes.before-restore-*/sonarr/data/sonarr.db-wal
  grep -q "restic restore latest:/volumes --target $DATA_DIR/volumes" "$STUB_LOG"
}

@test "restore without --overwrite keeps volumes in place" {
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  ! ls -d "$DATA_DIR"/volumes.before-restore-* 2>/dev/null || false
}

@test "restore uses the installation's own snapshots when it has any" {
  FAKE_OWN_SNAPSHOTS=1 run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  grep -q "restic restore latest:/volumes --host testinst --target" "$STUB_LOG"
}

@test "restore falls back to the latest snapshot of any host when the installation has none yet" {
  run "$BATS_TEST_DIRNAME/../scripts/restore.sh"
  [ "$status" -eq 0 ]
  grep -q "restic restore latest:/volumes --target" "$STUB_LOG"
  ! grep -q "restic restore .*--host" "$STUB_LOG" || false
}
