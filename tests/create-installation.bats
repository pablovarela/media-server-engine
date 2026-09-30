load helpers

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/config-template"
  cp -R "$BATS_TEST_DIRNAME/../config-template/." "$ENGINE_DIR/config-template/"
  rmdir "$CONFIG_DIR"
  make_stub git '
dir=""
if [ "$1" = -C ]; then dir=$2; shift 2; fi
case $1 in
  remote) echo "git@github.com:someone/media-server-engine.git" ;;
  init) mkdir -p "$dir/.git" ;;
esac'
  make_stub gh '
case "$1 $2" in
  "auth status") [ -z "${FAKE_GH_LOGGED_OUT:-}" ] ;;
  "repo view") [ -n "${FAKE_REPO_EXISTS:-}" ] ;;
esac'
  make_stub age-keygen 'if [ "$1" = -o ]; then printf "# public key: age1newpublic\nAGE-SECRET-KEY-NEW\n" > "$2"; echo "Public key: age1newpublic" >&2; fi'
  make_stub fake-configure ''
  make_stub fake-join ''
  export CONFIGURE_COMMAND=fake-configure JOIN_COMMAND=fake-join
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

@test "creating refuses an installation whose config repo exists" {
  FAKE_REPO_EXISTS=1 run create testinst < <(echo)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "someone/media-server-config-testinst"
  ! grep -q "^age-keygen" "$STUB_LOG" || false
}

@test "creating needs gh logged in" {
  FAKE_GH_LOGGED_OUT=1 run create testinst < <(echo)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "gh auth login"
}

@test "a new secrets key is added next to existing ones and shown once" {
  mkdir -p "$HOME/.config/sops/age"
  echo "AGE-SECRET-KEY-EXISTING" > "$HOME/.config/sops/age/keys.txt"
  run create testinst < <(echo)
  [ "$status" -eq 0 ]
  grep -q "AGE-SECRET-KEY-EXISTING" "$HOME/.config/sops/age/keys.txt"
  grep -q "AGE-SECRET-KEY-NEW" "$HOME/.config/sops/age/keys.txt"
  [ "$(echo "$output" | grep -c "AGE-SECRET-KEY-NEW")" -eq 1 ]
  [ "$(stat -f %Lp "$HOME/.config/sops/age/keys.txt" 2>/dev/null || stat -c %a "$HOME/.config/sops/age/keys.txt")" = 600 ]
}

@test "the config repo starts from the template with this engine's path filled in" {
  run create testinst < <(echo)
  [ -f "$CONFIG_DIR/images.yml" ]
  grep -q '"depNameTemplate": "someone/media-server-engine"' "$CONFIG_DIR/renovate.json"
  ! grep -rq ENGINE_REPOSITORY "$CONFIG_DIR" || false
  grep -q "age: age1newpublic" "$CONFIG_DIR/.sops.yaml"
}

@test "creating configures, publishes a private repo and then joins" {
  run create testinst < <(echo)
  [ "$status" -eq 0 ]
  grep -q "^fake-configure" "$STUB_LOG"
  grep -q "gh repo create someone/media-server-config-testinst --private --source $CONFIG_DIR --push" "$STUB_LOG"
  grep -q "^fake-join testinst$" "$STUB_LOG"
  [ "$(grep -n '^fake-configure' "$STUB_LOG" | cut -d: -f1)" -lt "$(grep -n 'gh repo create' "$STUB_LOG" | cut -d: -f1)" ]
}

@test "the Renovate app is pointed out for the new repo" {
  run create testinst < <(echo)
  echo "$output" | grep -q "github.com/apps/renovate"
}
