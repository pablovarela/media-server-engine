load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_restic_lock_stub
}

teardown() {
  teardown_stubs
}

@test "unlock removes only stale locks and shows the ones left" {
  make_stub restic 'source "$(dirname "$0")/restic-locks"'
  run "$BATS_TEST_DIRNAME/../scripts/unlock-backup.sh"
  [ "$status" -eq 0 ]
  grep -qx "restic unlock" "$STUB_LOG"
  ! grep -q "remove-all" "$STUB_LOG" || false
  echo "$output" | grep -q "laptop"
  echo "$output" | grep -q "ALL=1"
}

@test "unlock says so when no lock is left" {
  make_stub restic ''
  run "$BATS_TEST_DIRNAME/../scripts/unlock-backup.sh"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "no locks left"
}

@test "unlock --remove-all removes every lock" {
  make_stub restic ''
  run "$BATS_TEST_DIRNAME/../scripts/unlock-backup.sh" --remove-all
  [ "$status" -eq 0 ]
  grep -qx "restic unlock --remove-all" "$STUB_LOG"
}

@test "unlock refuses an option it does not know" {
  make_stub restic ''
  run "$BATS_TEST_DIRNAME/../scripts/unlock-backup.sh" --everything
  [ "$status" -ne 0 ]
  ! grep -q "restic unlock" "$STUB_LOG" || false
}
