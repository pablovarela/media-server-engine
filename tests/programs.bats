load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  make_stub restic 'exit 3'
  make_stub docker ''
}

teardown() {
  teardown_stubs
}

@test "each backup program finds its package and ends with the status of a failed command" {
  for program in backup-role restore unlock-backup; do
    case $program in
      backup-role) args=main-machine ;;
      *) args="" ;;
    esac
    run "$BATS_TEST_DIRNAME/../scripts/$program.py" $args
    [ "$status" -eq 3 ] || { echo "$program exited $status: $output"; false; }
    [[ $output != *Traceback* ]]
  done
}

@test "the backup and verify programs start and report a machine that is not the main" {
  make_stub restic "echo '[{\"time\":\"2026-09-30T04:30:14Z\",\"tags\":[\"machine:other\"]}]'"
  export MACHINE_ID_FILE="$STUB_DIR/machine-id" HEALTHCHECKS_PING_KEY=""
  echo this-machine > "$MACHINE_ID_FILE"
  for program in backup verify-backup claim-backup-main; do
    run "$BATS_TEST_DIRNAME/../scripts/$program.py" < /dev/null
    [ "$status" -eq 1 ] || { echo "$program exited $status: $output"; false; }
    [[ $output == *"another machine is testinst's main"* ]] || [[ $output == *"nothing was claimed"* ]]
    [[ $output != *Traceback* ]]
  done
}

@test "installed units that run backup.sh and verify-backup.sh reach the Python programs" {
  make_stub restic 'echo "parent=$(ps -o comm= -p $PPID)" >> "$STUB_LOG"; exit 3'
  export MACHINE_ID_FILE="$STUB_DIR/machine-id" HEALTHCHECKS_PING_KEY=""
  echo this-machine > "$MACHINE_ID_FILE"
  for program in backup verify-backup; do
    : > "$STUB_LOG"
    run "$BATS_TEST_DIRNAME/../scripts/$program.sh"
    grep -qi "^parent=.*python" "$STUB_LOG" || { echo "$program: $(cat "$STUB_LOG")"; false; }
  done
}
