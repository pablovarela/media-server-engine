load helpers

setup() {
  setup_stubs
}

teardown() {
  teardown_stubs
}

@test "the update program finds its package and ends with the status of a failed command" {
  make_stub git 'exit 3'
  run "$BATS_TEST_DIRNAME/../scripts/update.py"
  [ "$status" -eq 3 ]
  [[ $output != *Traceback* ]]
  grep -q "^git -C $ENGINE_DIR status --porcelain --untracked-files=no$" "$STUB_LOG"
}
