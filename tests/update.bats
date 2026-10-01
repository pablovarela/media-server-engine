load helpers

setup() {
  setup_stubs
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
  echo ENGINE_VERSION=v1.0.0 > "$CONFIG_DIR/engine.env"
  make_stub git '
dir=""
if [ "$1" = -C ]; then dir=$2; shift 2; fi
case $1 in
  status)
    if [ "$dir" = "$CONFIG_DIR" ]; then changes=${FAKE_CONFIG_STATUS:-}; else changes=${FAKE_ENGINE_STATUS:-}; fi
    case " $* " in *" --untracked-files=no "*) changes=$(printf "%s\n" "$changes" | grep -v "^??" || true) ;; esac
    printf "%s" "$changes" ;;
  describe) echo "${FAKE_ENGINE_TAG:-v1.0.0}" ;;
  rev-parse) if [ -n "${FAKE_MISSING_TAG:-}" ]; then exit 1; fi; echo abc123 ;;
  remote) if [ "$dir" = "$CONFIG_DIR" ] && [ -n "${FAKE_LOCAL_CONFIG:-}" ]; then exit 2; fi; echo git@github.com:someone/config.git ;;
esac'
  make_stub sops '
case "$*" in
  *vpn.sops.env*) echo "OPENVPN_USER=u" ;;
  *apps.sops.env*) printf "SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nPORTAINER_ADMIN_PASSWORD=pw 1\n" ;;
esac'
  make_compose_stub '
echo "pulled=${MEDIA_SERVER_PULLED:-}" >> "$STUB_LOG"
if [ "$2" = pull ] || [ "$2" = config ]; then echo "$2 profiles=${COMPOSE_PROFILES:-}" >> "$STUB_LOG"; fi
if [ "$2" = up ]; then echo "up profiles=${COMPOSE_PROFILES:-}" >> "$STUB_LOG"; fi
if [ "$2" = config ]; then echo "{\"services\": {\"sonarr\": {\"volumes\": [{\"type\": \"bind\", \"source\": \"$DATA_DIR/volumes/sonarr/data\"}, {\"type\": \"bind\", \"source\": \"$DATA_DIR/media/tvshows\"}]}}}"; fi
if [ "$2" = ps ] && [ "$4" = gluetun ]; then echo "gluetun-current"; fi
if [ "$1" = inspect ]; then echo "container:${FAKE_ATTACHED_TO:-gluetun-current}"; fi
if [ "$2" = up ] && [ "$3" = -d ] && [ "$4" = --remove-orphans ] && [ -n "${FAKE_UP_FAILS:-}" ]; then exit 1; fi'
  make_stub fake-check-stack 'if [ -n "${FAKE_STACK_UNSAFE:-}" ]; then exit 1; fi'
  make_stub fake-wire ''
  make_stub fake-prune ''
  export CHECK_STACK_COMMAND=fake-check-stack WIRE_COMMAND=fake-wire PRUNE_COMMAND=fake-prune
}

teardown() {
  teardown_stubs
}

update() {
  "$BATS_TEST_DIRNAME/../scripts/update.sh"
}

line_of() {
  grep -n "$1" "$STUB_LOG" | head -1 | cut -d: -f1
}

@test "update refuses local changes in the config repo before pulling" {
  FAKE_CONFIG_STATUS=" M images.yml" run update
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "not committed"
  ! grep -q "pull --ff-only" "$STUB_LOG" || false
}

@test "local changes to a local-only config are to be committed, and are shown" {
  FAKE_LOCAL_CONFIG=1 FAKE_CONFIG_STATUS=" M prowlarr.yml" run update
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "prowlarr.yml"
  echo "$output" | grep -q "git -C $CONFIG_DIR commit -am"
  ! echo "$output" | grep -qi "push" || false
}

@test "local changes to a config on github are to be committed and pushed" {
  FAKE_CONFIG_STATUS=" M prowlarr.yml" run update
  [ "$status" -ne 0 ]
  echo "$output" | grep -qi "push"
}

@test "a local-only config is used as it is, without pulling" {
  FAKE_LOCAL_CONFIG=1 run update
  [ "$status" -eq 0 ]
  ! grep -q "pull --ff-only" "$STUB_LOG" || false
  grep -q "docker compose up -d --remove-orphans" "$STUB_LOG"
}

@test "update refuses local changes in the engine before pulling" {
  FAKE_ENGINE_STATUS=" M scripts/update.sh" run update
  [ "$status" -ne 0 ]
  ! grep -q "pull --ff-only" "$STUB_LOG" || false
}

@test "update pulls the config repo, decrypts and brings the stack up" {
  run update
  [ "$status" -eq 0 ]
  grep -q "git -C $CONFIG_DIR pull --ff-only" "$STUB_LOG"
  grep -q "docker compose pull" "$STUB_LOG"
  grep -q "docker compose up -d --remove-orphans" "$STUB_LOG"
  [ "$(cat "$ENGINE_DIR/.secrets/vpn.env")" = "OPENVPN_USER=u" ]
  grep -q "^SONARR_API_KEY: \"s1\"$" "$ENGINE_DIR/.secrets/configarr/secrets.yml"
}

@test "decrypted secrets are readable only by the owner" {
  run update
  for f in .secrets/vpn.env .secrets/apps.env .secrets/configarr/secrets.yml .secrets/sonarr.env .secrets/radarr.env .secrets/prowlarr.env .secrets/portainer_admin; do
    [ "$(file_mode "$ENGINE_DIR/$f")" = "600" ]
  done
}

@test "each arr gets only its own api key, named as the app reads it" {
  run update
  [ "$(cat "$ENGINE_DIR/.secrets/sonarr.env")" = "SONARR__AUTH__APIKEY=s1" ]
  [ "$(cat "$ENGINE_DIR/.secrets/radarr.env")" = "RADARR__AUTH__APIKEY=r1" ]
  [ "$(cat "$ENGINE_DIR/.secrets/prowlarr.env")" = "PROWLARR__AUTH__APIKEY=p1" ]
}

@test "the portainer admin password file holds exactly the password" {
  run update
  [ "$(od -c "$ENGINE_DIR/.secrets/portainer_admin" | head -1)" = "$(printf 'pw 1' | od -c | head -1)" ]
}

@test "wiring-step images are pulled and their mounts created, but up leaves them to wiring" {
  run update
  [ "$status" -eq 0 ]
  grep -q "^pull profiles=wiring$" "$STUB_LOG"
  grep -q "^config profiles=wiring$" "$STUB_LOG"
  grep -q "^up profiles=$" "$STUB_LOG"
}

@test "update passes the installation's time zone to compose" {
  printf 'INSTALLATION_NAME=testinst\nTZ=Europe/London\n' > "$CONFIG_DIR/installation.env"
  run update
  grep -qx 'TZ=Europe/London' "$ENGINE_DIR/.env"
}

@test "update writes the docker socket group id for compose" {
  run update
  grep -qE '^DOCKER_GID=[0-9]+$' "$ENGINE_DIR/.env"
}

@test "update creates the bind-mount directories under the data directory" {
  run update
  [ -d "$DATA_DIR/volumes/sonarr/data" ]
  [ -d "$DATA_DIR/media/tvshows" ]
}

@test "update switches the engine to the version the config pins, then runs from it" {
  FAKE_ENGINE_TAG=v0.9.0 run update
  [ "$status" -eq 0 ]
  grep -q "git -C $ENGINE_DIR checkout -q --detach v1.0.0" "$STUB_LOG"
  [ "$(grep -c "pull --ff-only" "$STUB_LOG")" -eq 1 ]
  grep -q "pulled=1" "$STUB_LOG"
}

@test "update stays on the current engine when the pinned version does not exist" {
  FAKE_ENGINE_TAG=v0.9.0 FAKE_MISSING_TAG=1 run update
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "v1.0.0"
  ! grep -q "checkout" "$STUB_LOG" || false
  ! grep -q "docker compose up" "$STUB_LOG" || false
}

@test "update leaves the engine alone when it already runs the pinned version" {
  run update
  ! grep -q "checkout" "$STUB_LOG" || false
  ! grep -q "pulled=1" "$STUB_LOG" || false
}

@test "ENGINE_VERSION=local runs the checked-out engine without switching" {
  echo ENGINE_VERSION=local > "$CONFIG_DIR/engine.env"
  FAKE_ENGINE_TAG=v0.9.0 run update
  [ "$status" -eq 0 ]
  ! grep -q "checkout" "$STUB_LOG" || false
}

@test "update stops before bringing the stack up when the merged compose is unsafe" {
  FAKE_STACK_UNSAFE=1 run update
  [ "$status" -ne 0 ]
  ! grep -q "docker compose up" "$STUB_LOG" || false
}

@test "update wires the apps after the stack is up and prunes last" {
  run update
  [ "$(line_of 'docker compose up -d --remove-orphans')" -lt "$(line_of '^fake-wire')" ]
  [ "$(line_of '^fake-wire')" -lt "$(line_of '^fake-prune')" ]
}

@test "update recreates gluetun dependents attached to an old gluetun" {
  FAKE_ATTACHED_TO=gluetun-old run update
  [ "$status" -eq 0 ]
  grep -q "docker compose up -d --force-recreate --no-deps prowlarr flaresolverr deluge" "$STUB_LOG"
}

@test "update leaves gluetun dependents alone when attached to the current gluetun" {
  run update
  ! grep -q "force-recreate" "$STUB_LOG" || false
}

@test "update reattaches gluetun dependents and keeps images when bringing the stack up fails" {
  FAKE_UP_FAILS=1 FAKE_ATTACHED_TO=gluetun-old run update
  [ "$status" -ne 0 ]
  grep -q "force-recreate --no-deps prowlarr flaresolverr deluge" "$STUB_LOG"
  ! grep -q "^fake-prune" "$STUB_LOG" || false
  ! grep -q "^fake-wire" "$STUB_LOG" || false
}

@test "update waits for a running backup, and gives up with a message if it does not finish" {
  hold_backup_lock
  UPDATE_BACKUP_WAIT_SECONDS=2 UPDATE_BACKUP_POLL_SECONDS=1 run update
  release_held_backup_lock
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "backup is still running"
  ! grep -q "docker compose up" "$STUB_LOG" || false
}

@test "a lock left by a backup that no longer runs does not hold up the update" {
  echo 999999 > "$DATA_DIR/.backup.lock"
  UPDATE_BACKUP_WAIT_SECONDS=2 run update
  [ "$status" -eq 0 ]
}

@test "a lock naming a pid that another process now uses does not hold up the update" {
  echo $$ > "$DATA_DIR/.backup.lock"
  UPDATE_BACKUP_WAIT_SECONDS=2 UPDATE_BACKUP_POLL_SECONDS=1 run update
  [ "$status" -eq 0 ]
}

@test "an untracked file in the config, such as a compose override, stops the update" {
  FAKE_CONFIG_STATUS="?? compose.override.yml" run update
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "compose.override.yml"
  ! grep -q "docker compose up" "$STUB_LOG" || false
}
