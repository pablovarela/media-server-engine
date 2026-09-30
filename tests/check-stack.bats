setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  WORK=$(mktemp -d)
  export ENGINE_DIR="$WORK/engine" CONFIG_DIR="$WORK/config" DATA_DIR="$WORK/data"
  mkdir -p "$ENGINE_DIR/.secrets" "$CONFIG_DIR" "$DATA_DIR"
  cp "$REPO/docker-compose.yml" "$REPO/docker-compose.monitoring.yml" "$ENGINE_DIR/"
  cp -R "$REPO/scripts" "$ENGINE_DIR/"
  cp "$REPO"/config-template/images*.yml "$CONFIG_DIR/"
  : > "$ENGINE_DIR/.secrets/vpn.env"
  for file in sonarr.env radarr.env prowlarr.env portainer_admin; do : > "$ENGINE_DIR/.secrets/$file"; done
  echo "DOCKER_GID=0" > "$ENGINE_DIR/.env"
}

teardown() {
  rm -rf "$WORK"
}

@test "check-stack passes the config template" {
  run "$ENGINE_DIR/scripts/check-stack.sh"
  [ "$status" -eq 0 ]
}

@test "check-stack names an image that is not pinned to a digest" {
  sed -i.bak 's|^\(    image: lscr.io/linuxserver/sonarr:[^@]*\)@sha256:[0-9a-f]*|\1|' "$CONFIG_DIR/images.yml"
  run "$ENGINE_DIR/scripts/check-stack.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "sonarr"
  echo "$output" | grep -qi "digest"
}

@test "check-stack names an unpinned monitoring image" {
  sed -i.bak 's|^\(    image: grafana/grafana:[^@]*\)@sha256:[0-9a-f]*|\1|' "$CONFIG_DIR/images.monitoring.yml"
  run "$ENGINE_DIR/scripts/check-stack.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "grafana"
}

@test "check-stack names a service from the override that writes app state as root" {
  cat > "$CONFIG_DIR/compose.override.yml" <<YML
services:
  extra:
    image: example/extra:1@sha256:0000000000000000000000000000000000000000000000000000000000000000
    volumes:
      - \${DATA_DIR}/volumes/extra:/config
YML
  run "$ENGINE_DIR/scripts/check-stack.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "extra"
  echo "$output" | grep -q "1000"
}
