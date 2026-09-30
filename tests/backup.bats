load helpers

setup() {
  setup_stubs
  export HEALTHCHECK_BACKUP_URL=https://hc.example/abc
  make_stub curl ''
  make_compose_stub 'if [ "$2" = ps ]; then printf "jellyfin\nsonarr\n"; fi'
  make_stub restic 'if [ "$1" = backup ] && [ -n "${FAKE_RESTIC_BACKUP_FAILS:-}" ]; then exit 1; fi'
}

teardown() {
  teardown_stubs
}

line_of() {
  grep -n "$1" "$STUB_LOG" | head -1 | cut -d: -f1
}

@test "backup stops the stack, snapshots, restarts what was running, prunes and pings success" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  [ "$(line_of 'docker compose stop')" -lt "$(line_of 'restic backup')" ]
  [ "$(line_of 'restic backup')" -lt "$(line_of 'docker compose start jellyfin sonarr')" ]
  [ "$(line_of 'docker compose start')" -lt "$(line_of 'restic forget --retry-lock 2h --prune --keep-daily 7 --keep-weekly 4 --keep-monthly 6')" ]
  tail -1 "$STUB_LOG" | grep -q "curl .*https://hc.example/abc$"
}

@test "backup uses the excludes file and tags the snapshot" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "restic backup --retry-lock 2h --exclude-file .*scripts/backup-excludes.txt --tag nightly volumes" "$STUB_LOG"
}

@test "backup restarts services when restic fails" {
  FAKE_RESTIC_BACKUP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  grep -q "docker compose start jellyfin sonarr" "$STUB_LOG"
  ! grep -q "restic forget" "$STUB_LOG"
}

@test "backup pings fail on error" {
  FAKE_RESTIC_BACKUP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "curl .*https://hc.example/abc/fail" "$STUB_LOG"
}

@test "backup starts each service once" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$(grep -c 'docker compose start' "$STUB_LOG")" -eq 1 ]
}

@test "backup starts nothing when nothing was running" {
  make_compose_stub ''
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  ! grep -q "docker compose start" "$STUB_LOG"
}

@test "backup pings fail when restarting the stack fails" {
  make_compose_stub 'if [ "$2" = ps ]; then printf "jellyfin\n"; fi; if [ "$2" = start ]; then exit 1; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc.example/abc/fail" "$STUB_LOG"
}

@test "backup pings fail when it cannot even list the running services" {
  make_compose_stub 'if [ "$2" = ps ]; then exit 1; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc.example/abc/fail" "$STUB_LOG"
}

@test "backup waits for a restic lock held by the weekly verification" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "restic backup --retry-lock 2h" "$STUB_LOG"
  grep -q "restic forget --retry-lock 2h" "$STUB_LOG"
}

@test "backup snapshots volumes from the data directory" {
  make_stub restic 'if [ "$1" = backup ]; then echo "cwd=$PWD" >> "$STUB_LOG"; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "cwd=$(cd "$DATA_DIR" && pwd -P)\|cwd=$DATA_DIR" "$STUB_LOG"
}
