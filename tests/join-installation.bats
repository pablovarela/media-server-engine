load helpers

setup() {
  setup_stubs
  export HOME="$STUB_DIR/home"
  mkdir -p "$HOME"
  export INSTALL_DIR="$STUB_DIR/testinst"
  export ENGINE_DIR="$INSTALL_DIR/engine" CONFIG_DIR="$INSTALL_DIR/config" DATA_DIR="$INSTALL_DIR/data"
  mkdir -p "$ENGINE_DIR" "$CONFIG_DIR" "$DATA_DIR"
  make_stub git '
dir=""
if [ "$1" = -C ]; then dir=$2; shift 2; fi
case $1 in
  remote) if [ "$2" = get-url ]; then echo "git@github.com:someone/media-server-engine.git"; fi ;;
  clone) if [ "$2" = -q ]; then cp -R "$3" "$4"; else mkdir -p "${@: -1}/secrets"; echo INSTALLATION_NAME=testinst > "${@: -1}/installation.env"; fi ;;
  rev-parse) echo abc123 ;;
esac'
  make_stub sops '
case $1 in
  decrypt) grep -q "AGE-SECRET-KEY-GOOD" "$SOPS_AGE_KEY_FILE" 2>/dev/null || exit 1 ;;
  exec-env) shift 2; eval "$*" ;;
esac'
  make_stub restic 'if [ "$1" = snapshots ]; then if [ -n "${FAKE_NO_SNAPSHOTS:-}" ]; then echo "[]"; else echo "[{\"id\":\"s1\"}]"; fi; fi'
  make_stub systemctl ''
  make_stub age-keygen 'if [ "$1" = -y ]; then case "$(cat)" in AGE-SECRET-KEY-*) echo age1public ;; *) echo "error at line 1: unknown identity type" >&2; exit 1 ;; esac; fi'
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
  grep -q "^fake-deploy-keys someone/media-server-engine someone/media-server-config-testinst:write$" "$STUB_LOG"
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
  [ "$(file_mode "$HOME/.config/sops/age/keys.txt")" = 600 ]
}

@test "a key that cannot decrypt the config stops the join" {
  run join testinst < <(printf 'AGE-SECRET-KEY-WRONG\nn\n')
  [ "$status" -ne 0 ]
  ! grep -q "^fake-update" "$STUB_LOG" || false
}

@test "an existing key that decrypts the config is not asked for again" {
  mkdir -p "$HOME/.config/sops/age"
  echo AGE-SECRET-KEY-GOOD > "$HOME/.config/sops/age/keys.txt"
  run join testinst < <(printf 'n\n')
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -qi "paste" || false
}

@test "nothing is restored when the installation has no backups yet" {
  FAKE_NO_SNAPSHOTS=1 run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  ! grep -q "^fake-restore" "$STUB_LOG" || false
}

@test "SKIP_RESTORE keeps data that was moved into place" {
  SKIP_RESTORE=1 run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  ! grep -q "^fake-restore" "$STUB_LOG" || false
}

@test "answering yes makes the machine the main with backup timers" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\ny\n')
  grep -q "^fake-claim" "$STUB_LOG"
  grep -q "^fake-install-timers media-backup media-verify$" "$STUB_LOG"
}

@test "answering no leaves backups to the main" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  ! grep -q "^fake-claim" "$STUB_LOG" || false
  ! grep -q "media-backup" "$STUB_LOG" || false
}

@test "the main question defaults to no when another machine is the main" {
  FAKE_OTHER_MAIN=1 run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\n\n')
  ! grep -q "^fake-claim" "$STUB_LOG" || false
  echo "$output" | grep -q "another machine"
}

@test "without systemd the timers are skipped with instructions" {
  without_systemd
  PATH="$STUB_DIR:/usr/bin:/bin" run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  ! grep -q "^fake-install-timers" "$STUB_LOG" || false
  echo "$output" | grep -q "make update"
}


@test "run from an engine elsewhere, joining lays out the installation next to a copy of it" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$source_engine/"
  ENGINE_DIR=$source_engine INSTALL_DIR="$STUB_DIR/elsewhere" run "$source_engine/scripts/join-installation.sh" testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  [ -f "$STUB_DIR/elsewhere/engine/scripts/join-installation.sh" ]
  grep -q "git clone github-media-server-config-testinst:someone/media-server-config-testinst.git $STUB_DIR/elsewhere/config" "$STUB_LOG"
}

@test "joining refuses an installation name that is not letters, digits and dashes" {
  run join "Bad Name" < /dev/null
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "lowercase letters, digits and dashes"
}

existing_keys() {
  mkdir -p "$HOME/.config/sops/age"
  printf 'AGE-SECRET-KEY-OTHER-INSTALLATION\n' > "$HOME/.config/sops/age/keys.txt"
}

@test "something that is not an age key is refused before the keys file is touched" {
  existing_keys
  run join testinst < <(printf 'not a key at all\nn\n')
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "not an age secrets key"
  [ "$(cat "$HOME/.config/sops/age/keys.txt")" = "AGE-SECRET-KEY-OTHER-INSTALLATION" ]
}

@test "a key that cannot decrypt the config leaves the keys file as it was" {
  existing_keys
  run join testinst < <(printf 'AGE-SECRET-KEY-WRONG\nn\n')
  [ "$status" -ne 0 ]
  [ "$(cat "$HOME/.config/sops/age/keys.txt")" = "AGE-SECRET-KEY-OTHER-INSTALLATION" ]
}

@test "spaces and a carriage return pasted around the key are dropped" {
  run join testinst < <(printf '  AGE-SECRET-KEY-GOOD \r\nn\n')
  [ "$status" -eq 0 ]
  grep -qx "AGE-SECRET-KEY-GOOD" "$HOME/.config/sops/age/keys.txt"
}

@test "the engine fetches its releases through its own deploy key" {
  run join testinst < <(printf 'AGE-SECRET-KEY-GOOD\nn\n')
  [ "$status" -eq 0 ]
  grep -q "^git -C $ENGINE_DIR remote set-url origin github-media-server-engine:someone/media-server-engine.git$" "$STUB_LOG"
  [ "$(grep -n 'remote set-url origin github-' "$STUB_LOG" | cut -d: -f1)" -gt "$(grep -n '^fake-deploy-keys' "$STUB_LOG" | cut -d: -f1)" ]
}
