load helpers

setup() {
  setup_stubs
  HOME_DIR=$(mktemp -d)
  mkdir -p "$HOME_DIR/engine/scripts" "$HOME_DIR/config"
  cp "$BATS_TEST_DIRNAME/../scripts/lib.sh" "$HOME_DIR/engine/scripts/"
  printf 'INSTALLATION_NAME=testinst\n' > "$HOME_DIR/config/installation.env"
  make_stub docker ''
}

teardown() {
  rm -rf "$HOME_DIR"
  teardown_stubs
}

in_lib() {
  bash -c "unset ENGINE_DIR CONFIG_DIR DATA_DIR; source '$HOME_DIR/engine/scripts/lib.sh'; $1"
}

@test "lib derives config and data beside the engine" {
  run in_lib 'echo "$CONFIG_DIR|$DATA_DIR"'
  [ "$output" = "$HOME_DIR/config|$HOME_DIR/data" ]
}

@test "config and data locations can be overridden" {
  run bash -c "export CONFIG_DIR=/c DATA_DIR=/d; source '$HOME_DIR/engine/scripts/lib.sh'; echo \"\$CONFIG_DIR|\$DATA_DIR\""
  [ "$output" = "/c|/d" ]
}

@test "load_installation exports the installation name" {
  run in_lib 'load_installation; echo "$INSTALLATION_NAME"'
  [ "$output" = testinst ]
}

@test "load_installation fails without an installation name" {
  : > "$HOME_DIR/config/installation.env"
  run in_lib 'load_installation'
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "INSTALLATION_NAME"
}

@test "stack_compose merges the engine compose with the config's images and override" {
  touch "$HOME_DIR/config/images.yml" "$HOME_DIR/config/compose.override.yml"
  run in_lib 'stack_compose ps'
  grep -q "docker compose --project-name media-server --project-directory $HOME_DIR/engine --env-file $HOME_DIR/engine/.env -f $HOME_DIR/engine/docker-compose.yml -f $HOME_DIR/config/images.yml -f $HOME_DIR/config/compose.override.yml ps" "$STUB_LOG"
}

@test "stack_compose leaves out a missing override" {
  touch "$HOME_DIR/config/images.yml"
  run in_lib 'stack_compose ps'
  ! grep -q "compose.override.yml" "$STUB_LOG" || false
}

@test "monitoring_compose uses the monitoring images file" {
  touch "$HOME_DIR/config/images.monitoring.yml"
  run in_lib 'monitoring_compose ps'
  grep -q "docker compose --project-name monitoring .* -f $HOME_DIR/engine/docker-compose.monitoring.yml -f $HOME_DIR/config/images.monitoring.yml ps" "$STUB_LOG"
}

@test "tests run with a throwaway home and no real key file" {
  [ "$HOME" = "$STUB_DIR/home" ]
  [ -z "${SOPS_AGE_KEY_FILE:-}" ]
}

@test "installation names are lowercase letters, digits and dashes" {
  source "$BATS_TEST_DIRNAME/../scripts/lib.sh"
  for good in trial media-2 a; do valid_installation_name "$good"; done
  for bad in "" Trial "trialpub=age1x" "-trial" "a b" "a/b" "$(printf 'x%.0s' $(seq 41))"; do
    ! valid_installation_name "$bad" || { echo "accepted: $bad"; false; }
  done
}

@test "outside an installation, load_installation names the installations on this machine" {
  rm "$HOME_DIR/config/installation.env"
  mkdir -p "$HOME/trial/engine" "$HOME/trial/config" "$HOME/other/engine"
  echo INSTALLATION_NAME=trial > "$HOME/trial/config/installation.env"
  run in_lib 'load_installation'
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "not an installation"
  echo "$output" | grep -qx "  cd $HOME/trial"
  ! echo "$output" | grep -q "other" || false
  echo "$output" | grep -q "make create-installation NAME="
}
