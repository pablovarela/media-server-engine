load helpers

setup() {
  setup_stubs
  printf '    volumes:\n      - ./volumes/sonarr/data:/config\n      - ./media/tvshows:/tv\n      - ./downloads:/downloads\n      - ./prometheus/:/etc/prometheus/\n' > "$MEDIA_SERVER_DIR/docker-compose.yml"
  make_stub git '
if [ "$1" = status ]; then printf "%s" "${FAKE_GIT_STATUS:-}"; fi
if [ "$1" = rev-parse ]; then
  if [ -n "${FAKE_HEAD_MOVES:-}" ] && [ "$(grep -c "^git pull" "$STUB_LOG")" -ge 1 ] && [ -z "${MEDIA_SERVER_PULLED:-}" ]; then echo new-head; else echo old-head; fi
fi'
  make_stub sops 'echo "SECRET=x"'
  make_stub docker '
echo "pulled=${MEDIA_SERVER_PULLED:-}" >> "$STUB_LOG"
if [ "$2" = ps ] && [ "$4" = gluetun ]; then echo "gluetun-current"; fi
if [ "$1" = inspect ]; then echo "container:${FAKE_ATTACHED_TO:-gluetun-current}"; fi
if [ "$2" = up ] && [ ! -d "$MEDIA_SERVER_DIR/volumes/sonarr/data" ]; then
  echo "volumes dir missing at up" >&2
  exit 9
fi
if [ "$2" = up ] && [ "$3" = -d ] && [ "$4" = --remove-orphans ] && [ -n "${FAKE_UP_FAILS:-}" ]; then exit 1; fi'
}

teardown() {
  teardown_stubs
}

@test "deploy refuses a dirty tree" {
  FAKE_GIT_STATUS=" M docker-compose.yml" run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "uncommitted changes"
  ! grep -q "git pull" "$STUB_LOG"
  ! grep -q "docker compose up" "$STUB_LOG"
}

@test "deploy pulls, decrypts and brings the stack up" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  grep -q "git pull --ff-only" "$STUB_LOG"
  grep -q "docker compose pull" "$STUB_LOG"
  grep -q "docker compose up -d --remove-orphans" "$STUB_LOG"
  [ "$(cat "$MEDIA_SERVER_DIR/.secrets/vpn.env")" = "SECRET=x" ]
  [ -f "$MEDIA_SERVER_DIR/volumes/configarr/config/secrets.yml" ]
}

@test "decrypted secrets are readable only by the owner" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$(stat -f %Lp "$MEDIA_SERVER_DIR/.secrets/vpn.env" 2>/dev/null || stat -c %a "$MEDIA_SERVER_DIR/.secrets/vpn.env")" = "600" ]
  [ "$(stat -f %Lp "$MEDIA_SERVER_DIR/volumes/configarr/config/secrets.yml" 2>/dev/null || stat -c %a "$MEDIA_SERVER_DIR/volumes/configarr/config/secrets.yml")" = "600" ]
}

@test "deploy creates the app state, media and download directories before starting containers" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  [ -d "$MEDIA_SERVER_DIR/volumes/sonarr/data" ]
  [ -d "$MEDIA_SERVER_DIR/media/tvshows" ]
  [ -d "$MEDIA_SERVER_DIR/downloads" ]
  [ ! -d "$MEDIA_SERVER_DIR/prometheus" ]
}

@test "deploy writes the docker socket group id for compose" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  grep -qE '^DOCKER_GID=[0-9]+$' "$MEDIA_SERVER_DIR/.env"
}

@test "deploy recreates gluetun dependents attached to an old gluetun" {
  FAKE_ATTACHED_TO=gluetun-old run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  grep -q "docker compose up -d --force-recreate --no-deps prowlarr flaresolverr deluge" "$STUB_LOG"
}

@test "deploy leaves gluetun dependents alone when attached to the current gluetun" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  ! grep -q "force-recreate" "$STUB_LOG"
}

@test "deploy reattaches gluetun dependents even when bringing the stack up fails" {
  FAKE_UP_FAILS=1 FAKE_ATTACHED_TO=gluetun-old run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -ne 0 ]
  grep -q "docker compose up -d --force-recreate --no-deps prowlarr flaresolverr deluge" "$STUB_LOG"
}

@test "deploy prunes outdated stack images after a successful deploy" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  grep -q "docker image ls" "$STUB_LOG"
}

@test "deploy keeps images when bringing the stack up fails" {
  FAKE_UP_FAILS=1 run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  ! grep -q "docker image ls" "$STUB_LOG"
}

@test "deploy restarts itself after a pull that moved the checkout" {
  FAKE_HEAD_MOVES=1 run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  [ "$(grep -c "^git pull" "$STUB_LOG")" -eq 1 ]
  grep -q "pulled=1" "$STUB_LOG"
  ! grep -q "pulled=$" "$STUB_LOG"
}

@test "deploy carries on without restarting when the pull brought nothing" {
  run "$BATS_TEST_DIRNAME/../scripts/deploy.sh"
  [ "$status" -eq 0 ]
  ! grep -q "pulled=1" "$STUB_LOG"
}
