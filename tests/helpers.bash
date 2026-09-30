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
