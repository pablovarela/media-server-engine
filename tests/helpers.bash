setup_stubs() {
  STUB_DIR=$(mktemp -d)
  STUB_LOG="$STUB_DIR/calls.log"
  ENGINE_DIR=$(mktemp -d)
  CONFIG_DIR=$(mktemp -d)
  DATA_DIR=$(mktemp -d)
  export STUB_LOG ENGINE_DIR CONFIG_DIR DATA_DIR
  export PATH="$STUB_DIR:$PATH"
  export HOME="$STUB_DIR/home"
  mkdir -p "$HOME"
  unset SOPS_AGE_KEY_FILE
  export WIRE_REQUEST_RETRY_SECONDS=0
  export SYSTEMD_RUNTIME_DIR="$STUB_DIR/systemd-running"
  export HEALTHCHECKS_API_URL=http://127.0.0.1:9/api/v3/checks/
  mkdir -p "$SYSTEMD_RUNTIME_DIR"
  : > "$STUB_LOG"
}

teardown_stubs() {
  rm -rf "$STUB_DIR" "$ENGINE_DIR" "$CONFIG_DIR" "$DATA_DIR"
}

make_stub() {
  local name=$1 body=$2
  cat > "$STUB_DIR/$name" <<EOF
#!/usr/bin/env bash
echo "$name \$*" >> "\$STUB_LOG"
$body
EOF
  chmod +x "$STUB_DIR/$name"
}

stub_log() {
  cat "$STUB_LOG"
}

make_compose_stub() {
  make_stub docker "
if [ \"\$1\" = compose ]; then
  shift
  while [ \$# -gt 0 ]; do
    case \$1 in
      --project-name|--project-directory|--env-file|-f) shift 2 ;;
      *) break ;;
    esac
  done
  echo \"docker compose \$*\" >> \"\$STUB_LOG\"
  set -- compose \"\$@\"
fi
$1"
}

start_fake_app() {
  FAKE_APP_STATE="$STUB_DIR/fake-app-state.json"
  FAKE_APP_WRITES="$STUB_DIR/fake-app-writes.jsonl"
  cp "$1" "$FAKE_APP_STATE"
  : > "$FAKE_APP_WRITES"
  rm -f "$STUB_DIR/fake-app-port"
  python3 "$BATS_TEST_DIRNAME/fake_app.py" "$FAKE_APP_STATE" "$FAKE_APP_WRITES" "$STUB_DIR/fake-app-port" &
  FAKE_APP_PID=$!
  local waited=0
  while [ ! -s "$STUB_DIR/fake-app-port" ] && [ "$waited" -lt 100 ]; do sleep 0.1; waited=$((waited + 1)); done
  [ -s "$STUB_DIR/fake-app-port" ] || { echo "the fake app did not start" >&2; return 1; }
  FAKE_APP_URL="http://127.0.0.1:$(cat "$STUB_DIR/fake-app-port")"
  export FAKE_APP_STATE FAKE_APP_WRITES FAKE_APP_URL
}

stop_fake_app() {
  [ -z "${FAKE_APP_PID:-}" ] || kill "$FAKE_APP_PID" 2>/dev/null || true
}

fake_app_writes() {
  python3 -c '
import json, sys
for line in open(sys.argv[1]):
    write = json.loads(line)
    print(write["method"], write["path"])' "$FAKE_APP_WRITES"
}

start_second_fake_app() {
  SECOND_APP_STATE="$STUB_DIR/second-app-state.json"
  SECOND_APP_WRITES="$STUB_DIR/second-app-writes.jsonl"
  cp "$1" "$SECOND_APP_STATE"
  : > "$SECOND_APP_WRITES"
  rm -f "$STUB_DIR/second-app-port"
  python3 "$BATS_TEST_DIRNAME/fake_app.py" "$SECOND_APP_STATE" "$SECOND_APP_WRITES" "$STUB_DIR/second-app-port" &
  SECOND_APP_PID=$!
  local waited=0
  while [ ! -s "$STUB_DIR/second-app-port" ] && [ "$waited" -lt 100 ]; do sleep 0.1; waited=$((waited + 1)); done
  [ -s "$STUB_DIR/second-app-port" ] || { echo "the second fake app did not start" >&2; return 1; }
  SECOND_APP_URL="http://127.0.0.1:$(cat "$STUB_DIR/second-app-port")"
  export SECOND_APP_STATE SECOND_APP_WRITES SECOND_APP_URL
}

stop_second_fake_app() {
  [ -z "${SECOND_APP_PID:-}" ] || kill "$SECOND_APP_PID" 2>/dev/null || true
}

file_mode() {
  stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1"
}

without_systemd() {
  rm -rf "$SYSTEMD_RUNTIME_DIR" "$STUB_DIR/systemctl"
}

hold_backup_lock() {
  python3 -c '
import fcntl, sys, time
lock = open(sys.argv[1], "a")
fcntl.flock(lock, fcntl.LOCK_EX)
lock.truncate(0)
lock.write(sys.argv[2])
lock.flush()
open(sys.argv[3], "w").close()
time.sleep(60)
' "$DATA_DIR/.backup.lock" "${1:-4242}" "$STUB_DIR/lock-held" &
  LOCK_HOLDER=$!
  for _ in $(seq 1 50); do
    [ -e "$STUB_DIR/lock-held" ] && return 0
    sleep 0.1
  done
  echo "the test could not take the backup lock" >&2
  return 1
}

release_held_backup_lock() {
  kill "$LOCK_HOLDER"
  wait "$LOCK_HOLDER" 2>/dev/null || true
}

backup_lock_is_held() {
  python3 -c '
import fcntl, sys
with open(sys.argv[1], "a") as lock:
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        sys.exit(0)
sys.exit(1)
' "$DATA_DIR/.backup.lock" 2>/dev/null
}

make_restic_lock_stub() {
  cat > "$STUB_DIR/restic-locks" <<'STUB'
case "$1 $2" in
  "list locks") echo 5f3a9c ;;
  "cat lock") echo '{"time":"2026-09-30T04:37:23+01:00","exclusive":false,"hostname":"laptop","pid":6116}' ;;
esac
STUB
}
