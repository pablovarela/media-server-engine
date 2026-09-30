load helpers

setup() {
  setup_stubs
  LIB="$BATS_TEST_DIRNAME/../scripts/lib.sh"
  make_stub curl ''
  make_stub hostname 'echo laptop'
  export INSTALLATION_NAME=testinst HEALTHCHECKS_PING_KEY=pingkey123
}

teardown() {
  teardown_stubs
}

@test "the backup check is named after the installation" {
  run bash -c "source '$LIB'; healthcheck_url backup"
  [ "$output" = "https://hc-ping.com/pingkey123/testinst-backup" ]
}

@test "the main's update check has no machine suffix" {
  run bash -c "source '$LIB'; MACHINE_ROLE=main healthcheck_url update"
  [ "$output" = "https://hc-ping.com/pingkey123/testinst-update" ]
}

@test "a secondary's update check carries its hostname" {
  run bash -c "source '$LIB'; MACHINE_ROLE=secondary healthcheck_url update"
  [ "$output" = "https://hc-ping.com/pingkey123/testinst-update-laptop" ]
}

@test "pings create the check on first use and put the suffix before the query" {
  run bash -c "source '$LIB'; ping_healthcheck backup /fail"
  grep -q "curl .*https://hc-ping.com/pingkey123/testinst-backup/fail?create=1$" "$STUB_LOG"
}

@test "success pings have no suffix" {
  run bash -c "source '$LIB'; ping_healthcheck verify"
  grep -q "curl .*https://hc-ping.com/pingkey123/testinst-verify?create=1$" "$STUB_LOG"
}

@test "without a ping key nothing is sent and a warning is logged" {
  run bash -c "source '$LIB'; HEALTHCHECKS_PING_KEY= ping_healthcheck backup /start"
  [ "$status" -eq 0 ]
  ! grep -q "^curl" "$STUB_LOG"
  echo "$output" | grep -qi "no healthchecks ping key"
}

@test "a failed ping never fails the caller" {
  make_stub curl 'exit 7'
  run bash -c "set -e; source '$LIB'; ping_healthcheck backup; echo still-running"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q still-running
}
