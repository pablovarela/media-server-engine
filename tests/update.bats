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

@test "an engine that re-runs update.sh after switching version reaches the Python update, already pulled" {
  make_stub git 'echo "pulled=${MEDIA_SERVER_PULLED:-} parent=$(ps -o comm= -p $PPID)" >> "$STUB_LOG"; exit 3'
  MEDIA_SERVER_PULLED=1 run "$BATS_TEST_DIRNAME/../scripts/update.sh"
  [ "$status" -eq 3 ]
  grep -qi "^pulled=1 parent=.*python" "$STUB_LOG"
  [[ $output != *Traceback* ]]
}
