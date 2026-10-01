load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub sops 'if [ "$1" = exec-env ]; then shift 2; eval "$*"; fi'
  make_stub restic 'echo "[{\"id\":\"s1\"}]"'
  make_stub systemctl ''
  for step in check-tools restore update install-timers claim; do make_stub "fake-$step" ''; done
  make_stub fake-role '[ "$1" = is-main ]'
  make_stub fake-apps 'echo "Jellyfin     http://homeserver.local:8096"'
  export APPS_COMMAND=fake-apps
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
  without_systemd
  PATH="$STUB_DIR:/usr/bin:/bin" run setup_machine < <(echo y)
  [ "$status" -eq 0 ]
  grep -q "^fake-claim" "$STUB_LOG"
  ! grep -q "^fake-install-timers" "$STUB_LOG" || false
}

@test "setup ends with a summary of where the installation lives and how to use it" {
  run setup_machine < <(echo y)
  [ "$status" -eq 0 ]
  summary=$(echo "$output" | sed -n '/testinst is ready/,$p')
  [ -n "$summary" ]
  echo "$summary" | grep -q "cd $(cd "$ENGINE_DIR" && pwd)"
  echo "$summary" | grep -q "http://homeserver.local:8096"
  echo "$summary" | grep -q "make logins"
  echo "$summary" | grep -q "make configure"
  echo "$summary" | grep -q "05:00"
  echo "$summary" | grep -q "04:30"
}

@test "without systemd the summary says nothing runs on its own" {
  without_systemd
  PATH="$STUB_DIR:/usr/bin:/bin" run setup_machine < <(echo n)
  summary=$(echo "$output" | sed -n '/testinst is ready/,$p')
  echo "$summary" | grep -q "nothing runs on its own"
  echo "$summary" | grep -q "make update"
  ! echo "$summary" | grep -q "04:30" || false
}

@test "a machine that is not the main is told the main backs up" {
  run setup_machine < <(echo n)
  summary=$(echo "$output" | sed -n '/testinst is ready/,$p')
  echo "$summary" | grep -q "Another machine backs up"
}

@test "a machine that is not the main is never told to back up" {
  without_systemd
  PATH="$STUB_DIR:/usr/bin:/bin" run setup_machine < <(echo n)
  summary=$(echo "$output" | sed -n '/testinst is ready/,$p')
  ! echo "$summary" | grep -q "backup-now" || false
  echo "$summary" | grep -q "run make update after config changes"
}

@test "the summary names the machine as the network knows it" {
  make_stub uname 'echo Darwin'
  make_stub scutil 'echo bonjour-name'
  run setup_machine < <(echo n)
  echo "$output" | grep -q "testinst is ready on bonjour-name.local"
}
