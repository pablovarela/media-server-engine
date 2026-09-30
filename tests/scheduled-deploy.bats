load helpers

setup() {
  setup_stubs
  export HEALTHCHECK_DEPLOY_URL=https://hc.example/deploy
  make_stub curl ''
  make_stub fake-check-tools 'if [ -n "${FAKE_TOOLS_BROKEN:-}" ]; then exit 1; fi'
  make_stub fake-deploy 'if [ -n "${FAKE_DEPLOY_FAILS:-}" ]; then exit 1; fi'
  export CHECK_TOOLS_COMMAND=fake-check-tools DEPLOY_COMMAND=fake-deploy
}

teardown() {
  teardown_stubs
}

@test "scheduled deploy checks the tools, deploys and pings success" {
  run "$BATS_TEST_DIRNAME/../scripts/scheduled-deploy.sh"
  [ "$status" -eq 0 ]
  [ "$(sed -n 1p "$STUB_LOG")" = "curl -fsS -m 10 --retry 3 -o /dev/null https://hc.example/deploy/start" ]
  grep -q "^fake-check-tools" "$STUB_LOG"
  grep -q "^fake-deploy" "$STUB_LOG"
  tail -1 "$STUB_LOG" | grep -q "curl .*https://hc.example/deploy$"
}

@test "scheduled deploy pings fail when the deploy fails" {
  FAKE_DEPLOY_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/scheduled-deploy.sh"
  [ "$status" -ne 0 ]
  grep -q "curl .*https://hc.example/deploy/fail" "$STUB_LOG"
}

@test "scheduled deploy does not deploy with broken tools" {
  FAKE_TOOLS_BROKEN=1 run "$BATS_TEST_DIRNAME/../scripts/scheduled-deploy.sh"
  [ "$status" -ne 0 ]
  ! grep -q "^fake-deploy" "$STUB_LOG"
  grep -q "curl .*https://hc.example/deploy/fail" "$STUB_LOG"
}
