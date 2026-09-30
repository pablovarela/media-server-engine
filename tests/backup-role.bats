load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  export MACHINE_ID_FILE="$STUB_DIR/machine-id"
  echo "this-machine" > "$MACHINE_ID_FILE"
  ROLE="$BATS_TEST_DIRNAME/../scripts/backup-role.sh"
}

teardown() {
  teardown_stubs
}

snapshots_tagged() {
  make_stub restic "echo '[{\"time\":\"2026-09-30T04:30:14Z\",\"hostname\":\"testinst\",\"tags\":[\"nightly\",\"machine:$1\"]}]'"
}

@test "the first machine of a new installation is the main" {
  make_stub restic "echo '[]'"
  run "$ROLE" is-main
  [ "$status" -eq 0 ]
}

@test "the machine that made the latest snapshot is the main" {
  snapshots_tagged this-machine
  run "$ROLE" is-main
  [ "$status" -eq 0 ]
}

@test "a machine is not the main when another made the latest snapshot" {
  snapshots_tagged other-machine
  run "$ROLE" is-main
  [ "$status" -eq 1 ]
}

@test "a machine is not the main when the repository cannot be read" {
  make_stub restic 'exit 1'
  run "$ROLE" is-main
  [ "$status" -ne 0 ]
  [ "$status" -ne 1 ] || false
}

@test "snapshots are looked up for the installation, not the machine hostname" {
  make_stub restic "echo '[]'"
  run "$ROLE" is-main
  grep -q "restic snapshots --host testinst --latest 1 --json" "$STUB_LOG"
}

@test "main-machine prints the latest snapshot's machine" {
  snapshots_tagged other-machine
  run "$ROLE" main-machine
  [ "$output" = other-machine ]
}

@test "machine-id reads the system machine id" {
  run "$ROLE" machine-id
  [ "$output" = this-machine ]
}

@test "machine-id generates and keeps an id when the system has none" {
  rm "$MACHINE_ID_FILE"
  run "$ROLE" machine-id
  first=$output
  [ -n "$first" ]
  run "$ROLE" machine-id
  [ "$output" = "$first" ]
}
