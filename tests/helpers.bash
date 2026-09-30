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
