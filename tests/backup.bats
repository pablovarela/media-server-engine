load helpers

setup() {
  setup_stubs
  export HEALTHCHECKS_PING_KEY=pk
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub curl ''
  make_compose_stub 'if [ "$2" = ps ]; then printf "jellyfin\nsonarr\n"; fi'
  make_stub restic 'if [ "$1" = backup ] && [ -n "${FAKE_RESTIC_BACKUP_FAILS:-}" ]; then exit 1; fi'
  make_stub fake-role '
case $1 in
  is-main) if [ -n "${FAKE_ROLE_UNREADABLE:-}" ]; then exit 2; fi; [ -z "${FAKE_SECONDARY:-}" ] ;;
  machine-id) echo this-machine ;;
esac'
  export BACKUP_ROLE_COMMAND=fake-role
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
  [ "$(line_of 'docker compose start')" -lt "$(line_of 'restic forget --retry-lock 2h --host testinst --prune --keep-daily 7 --keep-weekly 4 --keep-monthly 6')" ]
  tail -1 "$STUB_LOG" | grep -q "curl .*https://hc-ping.com/pk/testinst-backup?create=1$"
}

@test "backup uses the excludes file and tags the snapshot" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "restic backup --retry-lock 2h --host testinst --tag machine:this-machine --tag machine-name:.* --tag nightly --exclude-file .*scripts/backup-excludes.txt volumes" "$STUB_LOG"
}

@test "backup restarts services when restic fails" {
  FAKE_RESTIC_BACKUP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  grep -q "docker compose start jellyfin sonarr" "$STUB_LOG"
  ! grep -q "restic forget" "$STUB_LOG" || false
}

@test "backup pings fail on error" {
  FAKE_RESTIC_BACKUP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "curl .*https://hc-ping.com/pk/testinst-backup/fail?create=1" "$STUB_LOG"
}

@test "backup starts each service once" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$(grep -c 'docker compose start' "$STUB_LOG")" -eq 1 ]
}

@test "backup starts nothing when nothing was running" {
  make_compose_stub ''
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  ! grep -q "docker compose start" "$STUB_LOG" || false
}

@test "backup pings fail when restarting the stack fails" {
  make_compose_stub 'if [ "$2" = ps ]; then printf "jellyfin\n"; fi; if [ "$2" = start ]; then exit 1; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc-ping.com/pk/testinst-backup/fail?create=1" "$STUB_LOG"
}

@test "backup pings fail when it cannot even list the running services" {
  make_compose_stub 'if [ "$2" = ps ]; then exit 1; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc-ping.com/pk/testinst-backup/fail?create=1" "$STUB_LOG"
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

@test "a secondary never stops the stack or uploads, and reports why" {
  touch "$DATA_DIR/.backup-main"
  FAKE_SECONDARY=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  ! grep -q "docker compose stop" "$STUB_LOG" || false
  ! grep -q "restic backup" "$STUB_LOG" || false
  echo "$output" | grep -q "another machine is testinst's main"
  grep -q "testinst-backup/fail?create=1" "$STUB_LOG"
  [ ! -e "$DATA_DIR/.backup-main" ]
}

@test "a claim backs up even where another machine is the main" {
  CLAIM=1 FAKE_SECONDARY=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  grep -q "restic backup" "$STUB_LOG"
}

@test "a backup refuses to start while another one is running" {
  hold_backup_lock 4242
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  release_held_backup_lock
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "already running (process 4242)"
  ! grep -q "^restic backup" "$STUB_LOG" || false
  ! grep -q "docker compose stop" "$STUB_LOG" || false
  ! grep -q "/fail" "$STUB_LOG" || false
}

@test "a lock left by a backup that no longer runs is taken over" {
  echo 999999 > "$DATA_DIR/.backup.lock"
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  grep -q "^restic backup" "$STUB_LOG"
}

@test "a lock naming a pid that another process now uses, as after a reboot, is taken over" {
  echo $$ > "$DATA_DIR/.backup.lock"
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  grep -q "^restic backup" "$STUB_LOG"
}

@test "a backup holds the kernel lock while restic runs" {
  make_stub restic 'if [ "$1" = backup ]; then python3 -c "
import fcntl, sys
with open(sys.argv[1], \"a\") as lock:
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        print(\"lock held during backup\")
" "$DATA_DIR/.backup.lock" >> "$STUB_LOG"; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  grep -q "lock held during backup" "$STUB_LOG"
}

@test "the lock is released after a backup, whether it worked or not" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  ! backup_lock_is_held || false
  FAKE_RESTIC_BACKUP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  ! backup_lock_is_held || false
}

@test "a backup repository that cannot be read keeps this machine the main, and says so" {
  touch "$DATA_DIR/.backup-main"
  FAKE_ROLE_UNREADABLE=1 run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "cannot read the backup repository"
  ! echo "$output" | grep -q "another machine" || false
  [ -e "$DATA_DIR/.backup-main" ]
  ! grep -q "^restic backup" "$STUB_LOG" || false
}

@test "a successful backup marks this machine as the main again" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  [ -e "$DATA_DIR/.backup-main" ]
}

@test "a backup clears stale restic locks before it touches the repository" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 0 ]
  grep -qx "restic unlock" "$STUB_LOG"
  [ "$(line_of '^restic unlock')" -lt "$(line_of 'docker compose stop')" ]
  [ "$(line_of '^restic unlock')" -lt "$(line_of '^restic backup')" ]
}

@test "a backup that gives up waiting for a restic lock says who holds it and how to clear it" {
  make_restic_lock_stub
  make_stub restic 'source "$(dirname "$0")/restic-locks"; if [ "$1" = forget ]; then exit 11; fi'
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  [ "$status" -eq 11 ]
  echo "$output" | grep -q "laptop"
  echo "$output" | grep -q "6116"
  echo "$output" | grep -q "2026-09-30T04:37"
  echo "$output" | grep -q "make unlock-backup"
  grep -q "testinst-backup/fail?create=1" "$STUB_LOG"
}

@test "backup tags the snapshot with this machine's name, so people can tell the main apart" {
  run "$BATS_TEST_DIRNAME/../scripts/backup.sh"
  grep -q "restic backup .*--tag machine-name:$(hostname -s) " "$STUB_LOG"
}
