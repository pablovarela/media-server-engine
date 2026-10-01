load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub fake-backup 'echo "claim=${CLAIM:-}" >> "$STUB_LOG"; if [ -n "${FAKE_BACKUP_FAILS:-}" ]; then exit 1; fi'
  make_stub restic 'if [ "$1 $2" = "cat config" ] && [ -n "${FAKE_NO_REPOSITORY:-}" ]; then exit 10; fi'
  make_stub fake-role '
case $1 in
  is-main) [ -z "${FAKE_OTHER_MAIN:-}" ] ;;
  describe-main) echo "pi, last backup 2026-09-30 04:30" ;;
esac'
  export BACKUP_COMMAND=fake-backup BACKUP_ROLE_COMMAND=fake-role
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

@test "claiming creates the backup repository when it does not exist yet" {
  FAKE_NO_REPOSITORY=1 run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh"
  [ "$status" -eq 0 ]
  grep -q "^restic init$" "$STUB_LOG"
  [ "$(grep -n '^restic init$' "$STUB_LOG" | cut -d: -f1)" -lt "$(grep -n '^claim=1' "$STUB_LOG" | cut -d: -f1)" ]
}

@test "claiming leaves an existing backup repository as it is" {
  run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh"
  ! grep -q "^restic init" "$STUB_LOG" || false
}

@test "claiming from another main shows it and asks first; no changes nothing" {
  FAKE_OTHER_MAIN=1 run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh" < <(echo n)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "pi, last backup 2026-09-30 04:30"
  ! grep -q "claim=1" "$STUB_LOG" || false
  [ ! -e "$DATA_DIR/.backup-main" ]
}

@test "claiming from another main goes ahead when the answer is yes" {
  FAKE_OTHER_MAIN=1 run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh" < <(echo y)
  [ "$status" -eq 0 ]
  grep -q "claim=1" "$STUB_LOG"
}

@test "a claim already confirmed does not ask again" {
  FAKE_OTHER_MAIN=1 CLAIM_CONFIRMED=1 run "$BATS_TEST_DIRNAME/../scripts/claim-backup-main.sh" < /dev/null
  [ "$status" -eq 0 ]
  grep -q "claim=1" "$STUB_LOG"
}
