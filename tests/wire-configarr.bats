load helpers

setup() {
  setup_stubs
  make_compose_stub '
echo "dry=${FAKE_ENV_SEEN:-} $*" >> "$STUB_LOG"
[ -z "${FAKE_CONFIGARR_OUTPUT:-}" ] || printf "%s\n" "$FAKE_CONFIGARR_OUTPUT"
exit "${FAKE_CONFIGARR_STATUS:-0}"'
}

teardown() {
  teardown_stubs
}

wire_configarr() {
  "$BATS_TEST_DIRNAME/../scripts/wire/configarr.sh"
}

@test "configarr runs once as a throwaway container" {
  run wire_configarr
  [ "$status" -eq 0 ]
  grep -q "^docker compose run --rm configarr$" "$STUB_LOG"
}

@test "a dry run asks configarr to only report" {
  WIRE_DRY_RUN=1 run wire_configarr
  grep -q "^docker compose run --rm -e DRY_RUN=true configarr$" "$STUB_LOG"
}

@test "errors configarr only logs still fail the step" {
  FAKE_CONFIGARR_OUTPUT="ERROR [13:53:14.981]: Create download client 'Deluge' failed" run wire_configarr
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "Create download client"
}

@test "configarr exiting non-zero fails the step" {
  FAKE_CONFIGARR_STATUS=3 run wire_configarr
  [ "$status" -ne 0 ]
}

@test "passwords in configarr's report are hidden" {
  FAKE_CONFIGARR_OUTPUT="      fields.password: ******** -> hunter2-secret
      fields.Password: old -> other-secret
      fields.host: gluetun -> gluetun" run wire_configarr
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -q "hunter2-secret" || false
  ! echo "$output" | grep -q "other-secret" || false
  echo "$output" | grep -q "fields.password: (hidden)"
  echo "$output" | grep -q "fields.host: gluetun -> gluetun"
}
