load helpers

setup() {
  setup_stubs
  export HOME="$STUB_DIR/home" DEPLOY_KEY_POLL_SECONDS=0
  mkdir -p "$HOME"
  make_stub ssh-keygen 'while [ $# -gt 0 ]; do if [ "$1" = -f ]; then f=$2; fi; shift; done; echo private > "$f"; echo "ssh-ed25519 AAAAtest deploy" > "$f.pub"'
  make_stub hostname 'echo box'
  make_stub gh 'if [ "$1 $2" = "auth status" ] && [ -z "${FAKE_GH_LOGGED_IN:-}" ]; then exit 1; fi'
  make_stub ssh '
n=$(grep -c "^ssh " "$STUB_LOG")
if [ "$n" -ge "${FAKE_SSH_OK_AFTER:-2}" ]; then echo "Hi someone/repo! You have successfully authenticated, but GitHub does not provide shell access." >&2; fi
exit 1'
}

teardown() {
  teardown_stubs
}

keys() {
  "$BATS_TEST_DIRNAME/../scripts/deploy-keys.sh" someone/media-server-engine someone/media-server-config-testinst:write
}

@test "a key and an ssh alias are created for each repository" {
  run keys
  [ "$status" -eq 0 ]
  [ -f "$HOME/.ssh/media-server-engine-deploy" ]
  [ -f "$HOME/.ssh/media-server-config-testinst-deploy" ]
  grep -q "^Host github-media-server-engine$" "$HOME/.ssh/config"
  grep -q "IdentityFile $HOME/.ssh/media-server-config-testinst-deploy" "$HOME/.ssh/config"
}

@test "running again reuses the keys and the aliases" {
  keys >/dev/null 2>&1
  : > "$STUB_LOG"
  run keys
  ! grep -q "^ssh-keygen" "$STUB_LOG" || false
  [ "$(grep -c "^Host github-media-server-engine$" "$HOME/.ssh/config")" -eq 1 ]
}

@test "without gh the key and the GitHub page to add it are shown" {
  run keys
  echo "$output" | grep -q "ssh-ed25519 AAAAtest deploy"
  echo "$output" | grep -q "https://github.com/someone/media-server-engine/settings/keys/new"
  ! grep -q "deploy-key add" "$STUB_LOG" || false
}

@test "with gh logged in the keys are added for you, read-only for the engine" {
  FAKE_GH_LOGGED_IN=1 run keys
  grep -q "gh repo deploy-key add $HOME/.ssh/media-server-engine-deploy.pub --repo someone/media-server-engine --title" "$STUB_LOG"
  ! grep "repo someone/media-server-engine " "$STUB_LOG" | grep -q "allow-write" || false
}

config_key() {
  "$BATS_TEST_DIRNAME/../scripts/deploy-keys.sh" someone/media-server-config-testinst:write
}

@test "the config's key can write, so this machine can push what configure commits" {
  FAKE_GH_LOGGED_IN=1 run config_key
  grep -q "gh repo deploy-key add $HOME/.ssh/media-server-config-testinst-deploy.pub --repo someone/media-server-config-testinst --title .* --allow-write" "$STUB_LOG"
}

@test "without gh the config's key is shown with the write access it needs" {
  run config_key
  echo "$output" | grep -q "someone/media-server-config-testinst, with Allow write access ticked"
}

@test "it waits until GitHub accepts the key" {
  FAKE_SSH_OK_AFTER=3 run keys
  [ "$status" -eq 0 ]
  [ "$(grep -c "^ssh -T .*github-media-server-engine$" "$STUB_LOG")" -ge 3 ]
}
