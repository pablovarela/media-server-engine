load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub fake-backup 'echo "claim=${CLAIM:-}" >> "$STUB_LOG"; if [ -n "${FAKE_BACKUP_FAILS:-}" ]; then exit 1; fi'
  export BACKUP_COMMAND=fake-backup
}

teardown() {
  teardown_stubs
}

@test "claiming runs one backup that bypasses the main check" {
  run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh"
  [ "$status" -eq 0 ]
  grep -q "claim=1" "$STUB_LOG"
}

@test "claiming marks this machine as the main" {
  run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh"
  [ -e "$DATA_DIR/.backup-main" ]
  echo "$output" | grep -q "testinst's main"
}

@test "a failed claim leaves the machine unmarked" {
  FAKE_BACKUP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh"
  [ "$status" -ne 0 ]
  [ ! -e "$DATA_DIR/.backup-main" ]
}
