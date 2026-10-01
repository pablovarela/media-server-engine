load helpers

setup() {
  setup_stubs
  export HEALTHCHECKS_PING_KEY=pk
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub curl ''
  make_stub fake-role 'if [ -n "${FAKE_ROLE_UNREADABLE:-}" ]; then exit 2; fi; [ "$1" = is-main ] && [ -z "${FAKE_SECONDARY:-}" ]'
  export BACKUP_ROLE_COMMAND=fake-role
  make_stub restic '
if [ "$1" = restore ]; then
  target=$(echo "$*" | sed -E "s/.*--target ([^ ]+).*/\1/")
  mkdir -p "$target/app"
  printf "SQLite format 3\\000" > "$target/app/a.db"
  printf "SQLite format 3\\000" > "$target/app/b.sqlite"
  printf "\\000\\000\\020bolt" > "$target/app/portainer.db"
fi'
  make_stub sqlite3 'if [ -n "${FAKE_CORRUPT:-}" ] && [ "${1##*/}" = b.sqlite ]; then echo "*** in database main ***"; else echo ok; fi'
}

teardown() {
  teardown_stubs
}

@test "verify checks the repo and every restored database, then pings success" {
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -eq 0 ]
  grep -q "restic check" "$STUB_LOG"
  [ "$(grep -c 'sqlite3 .*PRAGMA integrity_check' "$STUB_LOG")" -eq 2 ]
  tail -1 "$STUB_LOG" | grep -q "curl .*https://hc-ping.com/pk/testinst-verify?create=1$"
}

@test "verify fails and pings fail when a database is corrupt" {
  FAKE_CORRUPT=1 run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "b.sqlite"
  grep -q "curl .*https://hc-ping.com/pk/testinst-verify/fail?create=1" "$STUB_LOG"
}

@test "verify fails when restic check fails" {
  make_stub restic 'if [ "$1" = check ]; then exit 1; fi'
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc-ping.com/pk/testinst-verify/fail?create=1" "$STUB_LOG"
}

@test "verify fails when the snapshot holds no databases" {
  make_stub restic ''
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc-ping.com/pk/testinst-verify/fail?create=1" "$STUB_LOG"
}

@test "verify removes its temporary restore" {
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  target=$(grep "restic restore" "$STUB_LOG" | sed -E "s/.*--target ([^ ]+).*/\1/")
  [ -n "$target" ]
  [ ! -e "$target" ]
}

@test "verify skips files that are not SQLite databases" {
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -eq 0 ]
  ! grep -q "sqlite3 .*portainer.db" "$STUB_LOG" || false
}

@test "verify names the database when sqlite3 itself fails" {
  make_stub sqlite3 'echo "Error: file is not a database" >&2; exit 26'
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -qE "cannot check app/(a.db|b.sqlite)"
  grep -q "curl .*https://hc-ping.com/pk/testinst-verify/fail?create=1" "$STUB_LOG"
}

@test "verify restores to disk-backed /var/tmp unless TMPDIR says otherwise" {
  run env -u TMPDIR "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -eq 0 ]
  grep -q "restic restore .*--target /var/tmp/media-verify\." "$STUB_LOG"
}

@test "verify waits for a restic lock held by the nightly backup" {
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  grep -q "restic check --retry-lock 2h" "$STUB_LOG"
  grep -q "restic restore .*--retry-lock 2h" "$STUB_LOG"
}

@test "a secondary does not verify the main's backups" {
  FAKE_SECONDARY=1 run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -ne 0 ]
  ! grep -q "restic check" "$STUB_LOG" || false
  echo "$output" | grep -q "another machine is testinst's main"
}

@test "a database file that is not sqlite is skipped without a warning" {
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -qi "null byte" || false
}

@test "a backup repository that cannot be read is reported as such, not as another main" {
  FAKE_ROLE_UNREADABLE=1 run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "cannot read the backup repository"
}

@test "verify clears stale restic locks before checking" {
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -eq 0 ]
  first=$(grep -n "^restic" "$STUB_LOG" | head -1)
  [ "${first#*:}" = "restic unlock" ]
}

@test "verify that gives up waiting for a restic lock says who holds it" {
  make_restic_lock_stub
  make_stub restic 'source "$(dirname "$0")/restic-locks"; if [ "$1" = check ]; then exit 11; fi'
  run "$BATS_TEST_DIRNAME/../scripts/verify-backup.sh"
  [ "$status" -eq 11 ]
  echo "$output" | grep -q "laptop"
  echo "$output" | grep -q "make unlock-backup"
}
