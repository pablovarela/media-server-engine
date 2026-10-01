load helpers

setup() {
  setup_stubs
  export INSTALL_DIR="$STUB_DIR/testinst"
  export ENGINE_DIR="$INSTALL_DIR/engine" CONFIG_DIR="$INSTALL_DIR/config" DATA_DIR="$INSTALL_DIR/data"
  mkdir -p "$ENGINE_DIR/config-template"
  cp -R "$BATS_TEST_DIRNAME/../config-template/." "$ENGINE_DIR/config-template/"
  make_stub git '
dir=""
if [ "$1" = -C ]; then dir=$2; shift 2; fi
case $1 in
  remote) if [ "$2" = get-url ]; then echo "git@github.com:someone/media-server-engine.git"; fi ;;
  init) mkdir -p "$dir/.git" ;;
  describe) if [ -n "${FAKE_ENGINE_TAG:-}" ]; then echo "$FAKE_ENGINE_TAG"; else exit 128; fi ;;
  clone) cp -R "$3" "$4" ;;
  rev-parse) echo abc123 ;;
  symbolic-ref) [ -z "${FAKE_DETACHED_ENGINE:-}" ] ;;
esac'
  make_stub gh '
case "$1 $2" in
  "auth status") [ -z "${FAKE_GH_LOGGED_OUT:-}" ] ;;
  "repo view") [ -n "${FAKE_REPO_EXISTS:-}" ] ;;
esac'
  make_stub age-keygen '
if [ "$1" = -o ]; then printf "# public key: age1newpublic\nAGE-SECRET-KEY-NEW\n" > "$2"; echo "Public key: age1newpublic" >&2; fi
if [ "$1" = -y ]; then case "$(cat)" in AGE-SECRET-KEY-NEW) echo age1newpublic ;; *) echo age1other ;; esac; fi'
  make_stub fake-bootstrap ''
  make_stub fake-configure '[ -z "${FAKE_CONFIGURE_FAILS:-}" ]'
  make_stub fake-setup-machine '[ -z "${FAKE_SETUP_FAILS:-}" ]'
  export BOOTSTRAP_COMMAND=fake-bootstrap CONFIGURE_COMMAND=fake-configure SETUP_MACHINE_COMMAND=fake-setup-machine
}

teardown() {
  teardown_stubs
}

create() {
  "$BATS_TEST_DIRNAME/../scripts/create-installation.sh" "$@"
}

@test "creating needs an installation name" {
  run create < /dev/null
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "NAME"
}

@test "creating refuses an installation whose config repo exists on github" {
  FAKE_REPO_EXISTS=1 run create testinst < <(echo)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "someone/media-server-config-testinst"
  ! grep -q "^age-keygen" "$STUB_LOG" || false
}

@test "creating works without the github cli, for a local-only config" {
  FAKE_GH_LOGGED_OUT=1 run create testinst < <(echo)
  [ "$status" -eq 0 ]
  grep -q "^fake-configure" "$STUB_LOG"
}

@test "a new secrets key is added next to existing ones and shown once" {
  mkdir -p "$HOME/.config/sops/age"
  echo "AGE-SECRET-KEY-EXISTING" > "$HOME/.config/sops/age/keys.txt"
  run create testinst < <(echo)
  [ "$status" -eq 0 ]
  grep -q "AGE-SECRET-KEY-EXISTING" "$HOME/.config/sops/age/keys.txt"
  grep -q "AGE-SECRET-KEY-NEW" "$HOME/.config/sops/age/keys.txt"
  [ "$(echo "$output" | grep -c "AGE-SECRET-KEY-NEW")" -eq 1 ]
  [ "$(file_mode "$HOME/.config/sops/age/keys.txt")" = 600 ]
}

@test "the config starts from the template with this engine's path filled in" {
  run create testinst < <(echo)
  [ -f "$CONFIG_DIR/images.yml" ]
  grep -q '"depNameTemplate": "someone/media-server-engine"' "$CONFIG_DIR/renovate.json"
  ! grep -rq ENGINE_REPOSITORY "$CONFIG_DIR" || false
  grep -q "age: age1newpublic" "$CONFIG_DIR/.sops.yaml"
}

@test "an engine on a release pins that release" {
  FAKE_ENGINE_TAG=v1.2.0 run create testinst < <(echo)
  grep -qx "ENGINE_VERSION=v1.2.0" "$CONFIG_DIR/engine.env"
}

@test "an engine between releases is used as it is" {
  run create testinst < <(echo)
  grep -qx "ENGINE_VERSION=local" "$CONFIG_DIR/engine.env"
}

@test "creating configures, then sets up this machine as a new installation" {
  run create testinst < <(echo)
  [ "$status" -eq 0 ]
  grep -q "^fake-configure" "$STUB_LOG"
  [ "$(grep -n '^fake-configure' "$STUB_LOG" | cut -d: -f1)" -lt "$(grep -n '^fake-setup-machine' "$STUB_LOG" | cut -d: -f1)" ]
  ! grep -q "^gh repo create" "$STUB_LOG" || false
}

@test "run from an engine elsewhere, creating lays out the installation next to a copy of it" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$BATS_TEST_DIRNAME/../config-template" "$source_engine/"
  ENGINE_DIR=$source_engine CONFIG_DIR="$STUB_DIR/checkout/config" DATA_DIR="$STUB_DIR/checkout/data" INSTALL_DIR="$STUB_DIR/newinst" \
    run "$source_engine/scripts/create-installation.sh" newinst < <(echo)
  [ "$status" -eq 0 ]
  [ -f "$STUB_DIR/newinst/engine/scripts/create-installation.sh" ]
  [ -f "$STUB_DIR/newinst/config/images.yml" ]
  [ ! -e "$STUB_DIR/checkout/config" ]
  echo "$output" | grep -q "$STUB_DIR/newinst"
}

@test "the installation directory defaults to one named after it in the home directory" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$BATS_TEST_DIRNAME/../config-template" "$source_engine/"
  unset INSTALL_DIR
  ENGINE_DIR=$source_engine run "$source_engine/scripts/create-installation.sh" newinst < <(echo)
  [ "$status" -eq 0 ]
  [ -f "$HOME/newinst/config/images.yml" ]
}

@test "an installation directory that already has an engine is not overwritten" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine" "$STUB_DIR/newinst/engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$BATS_TEST_DIRNAME/../config-template" "$source_engine/"
  ENGINE_DIR=$source_engine INSTALL_DIR="$STUB_DIR/newinst" run "$source_engine/scripts/create-installation.sh" newinst < <(echo)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "$STUB_DIR/newinst/engine already exists"
}

@test "an installation name that is not letters, digits and dashes is refused before anything is made" {
  run create "trialpub=age1x" < <(echo)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "lowercase letters, digits and dashes"
  ! grep -qE "^(git clone|age-keygen|fake-)" "$STUB_LOG" || false
}

@test "a create stopped before the settings are saved leaves nothing behind" {
  mkdir -p "$HOME/.config/sops/age"
  echo "AGE-SECRET-KEY-EXISTING" > "$HOME/.config/sops/age/keys.txt"
  FAKE_CONFIGURE_FAILS=1 run create testinst < <(echo)
  [ "$status" -ne 0 ]
  [ ! -e "$CONFIG_DIR" ]
  [ ! -e "$DATA_DIR" ]
  [ "$(cat "$HOME/.config/sops/age/keys.txt")" = "AGE-SECRET-KEY-EXISTING" ]
  echo "$output" | grep -q "nothing was kept"
}

@test "a stopped create that copied the engine removes the copy too, so it can be run again" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$BATS_TEST_DIRNAME/../config-template" "$source_engine/"
  FAKE_CONFIGURE_FAILS=1 ENGINE_DIR=$source_engine INSTALL_DIR="$STUB_DIR/newinst" run "$source_engine/scripts/create-installation.sh" newinst < <(echo)
  [ "$status" -ne 0 ]
  [ ! -e "$STUB_DIR/newinst" ]
  [ -d "$source_engine" ]
}

@test "a create that fails after the settings are saved keeps them and says how to finish" {
  FAKE_SETUP_FAILS=1 run create testinst < <(echo)
  [ "$status" -ne 0 ]
  [ -f "$CONFIG_DIR/images.yml" ]
  grep -q "AGE-SECRET-KEY-NEW" "$HOME/.config/sops/age/keys.txt"
  echo "$output" | grep -q "cd $ENGINE_DIR && make setup-machine"
}

@test "pressing ctrl-c during the questions also leaves nothing behind" {
  make_stub fake-configure 'kill -INT $PPID; sleep 1'
  run create testinst < <(echo)
  [ "$status" -eq 130 ]
  [ ! -e "$CONFIG_DIR" ]
  ! grep -q "AGE-SECRET-KEY-NEW" "$HOME/.config/sops/age/keys.txt" || false
}

@test "an engine copied from a branch stays on that branch, so it can be pulled" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$BATS_TEST_DIRNAME/../config-template" "$source_engine/"
  ENGINE_DIR=$source_engine INSTALL_DIR="$STUB_DIR/newinst" run "$source_engine/scripts/create-installation.sh" newinst < <(echo)
  [ "$status" -eq 0 ]
  ! grep -q "checkout -q abc123" "$STUB_LOG" || false
}

@test "an engine copied from a detached release checks out that exact commit" {
  source_engine="$STUB_DIR/checkout/media-server-engine"
  mkdir -p "$source_engine"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$BATS_TEST_DIRNAME/../config-template" "$source_engine/"
  FAKE_DETACHED_ENGINE=1 ENGINE_DIR=$source_engine INSTALL_DIR="$STUB_DIR/newinst" run "$source_engine/scripts/create-installation.sh" newinst < <(echo)
  [ "$status" -eq 0 ]
  grep -q "checkout -q abc123" "$STUB_LOG"
}

@test "a create undone after the key was shown says that key is no longer needed" {
  FAKE_CONFIGURE_FAILS=1 run create testinst < <(echo)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "no longer needed"
}

@test "removing the new key replaces keys.txt in one step and keeps it private" {
  mkdir -p "$HOME/.config/sops/age"
  echo "AGE-SECRET-KEY-EXISTING" > "$HOME/.config/sops/age/keys.txt"
  before=$(ls -i "$HOME/.config/sops/age/keys.txt" | awk '{print $1}')
  FAKE_CONFIGURE_FAILS=1 run create testinst < <(echo)
  [ "$(cat "$HOME/.config/sops/age/keys.txt")" = "AGE-SECRET-KEY-EXISTING" ]
  [ "$(ls -i "$HOME/.config/sops/age/keys.txt" | awk '{print $1}')" != "$before" ]
  [ "$(file_mode "$HOME/.config/sops/age/keys.txt")" = 600 ]
  [ -z "$(ls "$HOME/.config/sops/age" | grep -v '^keys.txt$')" ]
}
