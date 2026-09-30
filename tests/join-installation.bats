load helpers

setup() {
  setup_stubs
  export HOME="$STUB_DIR/home"
  mkdir -p "$HOME"
  make_stub git '
dir=""
if [ "$1" = -C ]; then dir=$2; shift 2; fi
case $1 in
  remote) echo "git@github.com:someone/media-server-engine.git" ;;
  clone) mkdir -p "${@: -1}/secrets"; echo INSTALLATION_NAME=testinst > "${@: -1}/installation.env" ;;
esac'
  make_stub sops '
case $1 in
  decrypt) grep -q "AGE-SECRET-KEY-GOOD" "$SOPS_AGE_KEY_FILE" 2>/dev/null || exit 1 ;;
  exec-env) shift 2; eval "$*" ;;
esac'
  make_stub restic 'if [ "$1" = snapshots ]; then if [ -n "${FAKE_NO_SNAPSHOTS:-}" ]; then echo "[]"; else echo "[{\"id\":\"s1\"}]"; fi; fi'
  make_stub systemctl ''
  for step in bootstrap deploy-keys check-tools restore update install-timers claim; do
    make_stub "fake-$step" ''
  done
  make_stub fake-role '[ "$1" = is-main ] && [ -z "${FAKE_OTHER_MAIN:-}" ]'
  export BOOTSTRAP_COMMAND=fake-bootstrap DEPLOY_KEYS_COMMAND=fake-deploy-keys CHECK_TOOLS_COMMAND=fake-check-tools \
    RESTORE_COMMAND=fake-restore UPDATE_COMMAND=fake-update INSTALL_TIMERS_COMMAND=fake-install-timers \
    CLAIM_COMMAND=fake-claim BACKUP_ROLE_COMMAND=fake-role
  rmdir "$CONFIG_DIR"
}

teardown() {
  teardown_stubs
}

join() {
  "$BATS_TEST_DIRNAME/../scripts/join-installation.sh" "$@"
}

line_of() {
  grep -n "$1" "$STUB_LOG" | head -1 | cut -d: -f1
}

@test "joining needs an installation name" {
  run join
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "NAME"
}

@test "joining sets up access, restores, updates and installs timers in order" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  grep -q "^fake-deploy-keys someone/media-server-engine someone/media-server-config-testinst$" "$STUB_LOG"
  grep -q "git clone github-media-server-config-testinst:someone/media-server-config-testinst.git $CONFIG_DIR" "$STUB_LOG"
  [ "$(line_of '^fake-bootstrap')" -lt "$(line_of '^fake-deploy-keys')" ]
  [ "$(line_of '^git clone')" -lt "$(line_of '^fake-check-tools')" ]
  [ "$(line_of '^fake-check-tools')" -lt "$(line_of '^fake-restore')" ]
  [ "$(line_of '^fake-restore')" -lt "$(line_of '^fake-update')" ]
  grep -q "^fake-install-timers media-update media-download-cleanup$" "$STUB_LOG"
}

@test "the pasted secrets key is stored privately and checked" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  grep -q "AGE-SECRET-KEY-GOOD" "$HOME/.config/sops/age/keys.txt"
  [ "$(stat -f %Lp "$HOME/.config/sops/age/keys.txt" 2>/dev/null || stat -c %a "$HOME/.config/sops/age/keys.txt")" = 600 ]
}

@test "a key that cannot decrypt the config stops the join" {
  run join testinst < <(printf 'AGE-SECRET-KEY-WRONG\nn\n')
  [ "$status" -ne 0 ]
  ! grep -q "^fake-update" "$STUB_LOG"
}

@test "an existing key that decrypts the config is not asked for again" {
  mkdir -p "$HOME/.config/sops/age"
  echo AGE-SECRET-KEY-GOOD > "$HOME/.config/sops/age/keys.txt"
  run join testinst < <(printf 'n\n')
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -qi "paste"
}

@test "nothing is restored when the installation has no backups yet" {
  FAKE_NO_SNAPSHOTS=1 run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  ! grep -q "^fake-restore" "$STUB_LOG"
}

@test "SKIP_RESTORE keeps data that was moved into place" {
  SKIP_RESTORE=1 run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  ! grep -q "^fake-restore" "$STUB_LOG"
}

@test "answering yes makes the machine the main with backup timers" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\ny\n')
  grep -q "^fake-claim" "$STUB_LOG"
  grep -q "^fake-install-timers media-backup media-verify$" "$STUB_LOG"
}

@test "answering no leaves backups to the main" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  ! grep -q "^fake-claim" "$STUB_LOG"
  ! grep -q "media-backup" "$STUB_LOG"
}

@test "the main question defaults to no when another machine is the main" {
  FAKE_OTHER_MAIN=1 run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\n\n')
  ! grep -q "^fake-claim" "$STUB_LOG"
  echo "$output" | grep -q "another machine"
}

@test "without systemd the timers are skipped with instructions" {
  rm "$STUB_DIR/systemctl"
  PATH="$STUB_DIR:/usr/bin:/bin" run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  ! grep -q "^fake-install-timers" "$STUB_LOG"
  echo "$output" | grep -q "make update"
}

