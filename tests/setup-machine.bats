load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub sops 'if [ "$1" = exec-env ]; then shift 2; eval "$*"; fi'
  make_stub restic 'echo "[{\"id\":\"s1\"}]"'
  make_stub systemctl ''
  for step in check-tools restore update install-timers claim; do make_stub "fake-$step" ''; done
  make_stub fake-role '[ "$1" = is-main ]'
  export CHECK_TOOLS_COMMAND=fake-check-tools RESTORE_COMMAND=fake-restore UPDATE_COMMAND=fake-update \
    INSTALL_TIMERS_COMMAND=fake-install-timers CLAIM_COMMAND=fake-claim BACKUP_ROLE_COMMAND=fake-role
}

teardown() {
  teardown_stubs
}

setup_machine() {
  "$BATS_TEST_DIRNAME/../scripts/setup-machine.sh"
}

@test "a new installation is not restored even if the repository has snapshots" {
  run setup_machine < <(echo y)
  [ "$status" -eq 0 ]
  ! grep -q "^fake-restore" "$STUB_LOG" || false
}

@test "a joining machine restores the latest backup before bringing the stack up" {
  RESTORE_FROM_BACKUP=1 run setup_machine < <(echo n)
  [ "$(grep -n '^fake-restore' "$STUB_LOG" | cut -d: -f1)" -lt "$(grep -n '^fake-update' "$STUB_LOG" | cut -d: -f1)" ]
}

@test "the main of a machine without systemd still claims, so manual backups work" {
  rm "$STUB_DIR/systemctl"
  PATH="$STUB_DIR:/usr/bin:/bin" run setup_machine < <(echo y)
  [ "$status" -eq 0 ]
  grep -q "^fake-claim" "$STUB_LOG"
  ! grep -q "^fake-install-timers" "$STUB_LOG" || false
}
