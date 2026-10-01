load helpers

setup() {
  setup_stubs
  make_stub docker 'if [ -n "${FAKE_HOMEPAGE_PORT:-}" ]; then echo "0.0.0.0:$FAKE_HOMEPAGE_PORT"; else exit 1; fi'
  PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
}

teardown() {
  [ -z "${LISTENER:-}" ] || kill "$LISTENER" 2>/dev/null || true
  teardown_stubs
}

listen() {
  python3 -c 'import socket, sys, time; s=socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind(("127.0.0.1", int(sys.argv[1]))); s.listen(); time.sleep(30)' "$PORT" &
  LISTENER=$!
  for _ in $(seq 1 50); do
    python3 -c 'import socket, sys; s=socket.socket(); sys.exit(s.connect_ex(("127.0.0.1", int(sys.argv[1]))))' "$PORT" && return 0
    sleep 0.1
  done
}

@test "a port something listens on is in use" {
  listen
  run "$BATS_TEST_DIRNAME/../scripts/port-in-use.sh" "$PORT"
  [ "$status" -eq 0 ]
}

@test "a port nothing listens on is free" {
  run "$BATS_TEST_DIRNAME/../scripts/port-in-use.sh" "$PORT"
  [ "$status" -ne 0 ]
}

@test "a port held by this installation's own landing page counts as free" {
  listen
  FAKE_HOMEPAGE_PORT=$PORT run "$BATS_TEST_DIRNAME/../scripts/port-in-use.sh" "$PORT"
  [ "$status" -ne 0 ]
}
