load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
}

teardown() {
  teardown_stubs
}

@test "engine-run finds its package from any directory and ends with a failed command's status" {
  make_stub restic 'exit 3'
  cd "$STUB_DIR"
  run "$BATS_TEST_DIRNAME/../scripts/engine-run" backup-role main-machine
  [ "$status" -eq 3 ]
  [[ $output != *Traceback* ]]
}

@test "engine-run without a command lists the commands" {
  run "$BATS_TEST_DIRNAME/../scripts/engine-run"
  [ "$status" -eq 1 ]
  [[ $output == *"usage: engine-run <command> [arguments]"* ]]
  [[ $output == *"remove-executable-downloads"* ]]
}

@test "tests run with a throwaway home and no real key file" {
  [ "$HOME" = "$STUB_DIR/home" ]
  [ -z "${SOPS_AGE_KEY_FILE:-}" ]
}
