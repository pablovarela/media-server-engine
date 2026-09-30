setup_stubs() {
  STUB_DIR=$(mktemp -d)
  STUB_LOG="$STUB_DIR/calls.log"
  MEDIA_SERVER_DIR=$(mktemp -d)
  export STUB_LOG MEDIA_SERVER_DIR
  export PATH="$STUB_DIR:$PATH"
  : > "$STUB_LOG"
}

teardown_stubs() {
  rm -rf "$STUB_DIR" "$MEDIA_SERVER_DIR"
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
