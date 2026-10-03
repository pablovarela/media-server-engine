load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  export HEALTHCHECKS_PING_KEY=pk
  make_stub curl ''
  make_stub hostname 'echo laptop'
  make_stub fake-check-tools 'if [ -n "${FAKE_TOOLS_BROKEN:-}" ]; then exit 1; fi'
  make_stub fake-update 'echo "update-env api=${HEALTHCHECKS_API_KEY:-} manage=${HEALTHCHECKS_MANAGE_KEY:-}" >> "$STUB_LOG"; if [ -n "${FAKE_UPDATE_FAILS:-}" ]; then exit 1; fi'
  make_stub fake-pinned-tools ''
  export CHECK_TOOLS_COMMAND=fake-check-tools UPDATE_COMMAND=fake-update PINNED_TOOLS_COMMAND=fake-pinned-tools
}

teardown() {
  teardown_stubs
}

scheduled_update() {
  "$BATS_TEST_DIRNAME/../scripts/scheduled-update.sh"
}

@test "the main checks the tools, updates and pings its update check" {
  touch "$DATA_DIR/.backup-main"
  run scheduled_update
  [ "$status" -eq 0 ]
  [ "$(sed -n 1p "$STUB_LOG")" = "curl -fsS -m 10 --retry 3 -o /dev/null https://hc-ping.com/pk/testinst-update/start?create=1" ]
  grep -q "^fake-check-tools" "$STUB_LOG"
  grep -q "^fake-update" "$STUB_LOG"
  tail -1 "$STUB_LOG" | grep -q "https://hc-ping.com/pk/testinst-update?create=1$"
}

@test "a secondary pings its own update check" {
  run scheduled_update
  grep -q "https://hc-ping.com/pk/testinst-update-laptop?create=1$" "$STUB_LOG"
}

@test "a failed update pings fail" {
  FAKE_UPDATE_FAILS=1 run scheduled_update
  [ "$status" -ne 0 ]
  grep -q "testinst-update-laptop/fail?create=1" "$STUB_LOG"
}

@test "broken tools stop the update and ping fail" {
  FAKE_TOOLS_BROKEN=1 run scheduled_update
  [ "$status" -ne 0 ]
  ! grep -q "^fake-update" "$STUB_LOG" || false
  grep -q "/fail?create=1" "$STUB_LOG"
}

@test "the scheduled update installs the pinned tools before checking them" {
  run scheduled_update
  [ "$status" -eq 0 ]
  [ "$(grep -n '^fake-pinned-tools' "$STUB_LOG" | cut -d: -f1)" -lt "$(grep -n '^fake-check-tools' "$STUB_LOG" | cut -d: -f1)" ]
}

@test "the update and everything it starts see the ping key but not the api keys" {
  HEALTHCHECKS_API_KEY=read-key HEALTHCHECKS_MANAGE_KEY=write-key run scheduled_update
  [ "$status" -eq 0 ]
  grep -qx "update-env api= manage=" "$STUB_LOG"
  grep -q "https://hc-ping.com/pk/testinst-update-laptop?create=1$" "$STUB_LOG"
}
