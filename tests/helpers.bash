setup_stubs() {
  STUB_DIR=$(mktemp -d)
  STUB_LOG="$STUB_DIR/calls.log"
  export STUB_LOG
  export PATH="$STUB_DIR:$PATH"
  export HOME="$STUB_DIR/home"
  mkdir -p "$HOME"
  : > "$STUB_LOG"
}

teardown_stubs() {
  rm -rf "$STUB_DIR"
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
