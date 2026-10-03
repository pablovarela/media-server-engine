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
  remote) if [ "$dir" = "$CONFIG_DIR" ] && [ -n "${FAKE_LOCAL_CONFIG:-}" ]; then exit 2; fi; if [ "$dir" = "$ENGINE_DIR" ]; then echo "${FAKE_ENGINE_REMOTE:-github-media-server-engine:someone/media-server-engine.git}"; else echo git@github.com:someone/config.git; fi ;;
esac'
  make_stub sops '
case "$*" in
  *vpn.sops.env*) echo "OPENVPN_USER=u" ;;
  *apps.sops.env*) printf "SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nPORTAINER_ADMIN_PASSWORD=pw 1\n" ;;
  *healthchecks.sops.env*) printf "HEALTHCHECKS_PING_KEY=ping\nHEALTHCHECKS_API_KEY=hc-read\n" ;;
esac'
  make_compose_stub '
echo "pulled=${MEDIA_SERVER_PULLED:-}" >> "$STUB_LOG"
if [ "$2" = pull ] || [ "$2" = config ]; then echo "$2 profiles=${COMPOSE_PROFILES:-}" >> "$STUB_LOG"; fi
if [ "$2" = up ]; then echo "up profiles=${COMPOSE_PROFILES:-}" >> "$STUB_LOG"; fi
if [ "$2" = config ]; then echo "{\"services\": {\"sonarr\": {\"volumes\": [{\"type\": \"bind\", \"source\": \"$DATA_DIR/volumes/sonarr/data\"}, {\"type\": \"bind\", \"source\": \"$DATA_DIR/media/tvshows\"}]}}}"; fi
if [ "$2" = ps ] && [ "$4" = gluetun ]; then echo "gluetun-current"; fi
if [ "$1" = inspect ]; then echo "container:${FAKE_ATTACHED_TO:-gluetun-current}"; fi
if [ "$2" = up ] && [ "$3" = -d ] && [ "$4" = --remove-orphans ] && [ -n "${FAKE_UP_FAILS:-}" ]; then exit 1; fi
if [ "$2" = pull ] && [ -n "${FAKE_PULL_ERROR:-}" ]; then
  refusals=$(cat "$BATS_TEST_TMPDIR/pull-refusals" 2>/dev/null || echo 0)
  if [ "$refusals" -lt "${FAKE_PULL_REFUSALS:-99}" ]; then
    echo $((refusals + 1)) > "$BATS_TEST_TMPDIR/pull-refusals"
    echo "Error response from daemon: $FAKE_PULL_ERROR" >&2
    exit 1
  fi
fi'
  make_stub fake-check-stack 'if [ -n "${FAKE_STACK_UNSAFE:-}" ]; then exit 1; fi'
  make_stub fake-wire ''
  make_stub fake-prune ''
  make_stub fake-pinned-tools ''
  make_stub fake-healthchecks 'echo "healthchecks-env ssh=$HEALTHCHECKS_SSH dir=$HEALTHCHECKS_DIRECTORY" >> "$STUB_LOG"; if [ -n "${FAKE_HEALTHCHECKS_FAILS:-}" ]; then exit 1; fi'
  export CHECK_STACK_COMMAND=fake-check-stack WIRE_COMMAND=fake-wire PRUNE_COMMAND=fake-prune PINNED_TOOLS_COMMAND=fake-pinned-tools HEALTHCHECKS_COMMAND=fake-healthchecks
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
  mkdir -p "$CONFIG_DIR/configarr" && echo "api_key: !secret SONARR_API_KEY" > "$CONFIG_DIR/configarr/config.yml"
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

@test "configarr gets only the secrets its config refers to" {
  mkdir -p "$CONFIG_DIR/configarr"
  printf 'api_key: !secret SONARR_API_KEY\nother: !secret RADARR_API_KEY\nagain: !secret SONARR_API_KEY\n' > "$CONFIG_DIR/configarr/config.yml"
  run update
  [ "$status" -eq 0 ]
  grep -q "^SONARR_API_KEY: \"s1\"$" "$ENGINE_DIR/.secrets/configarr/secrets.yml"
  grep -q "^RADARR_API_KEY: \"r1\"$" "$ENGINE_DIR/.secrets/configarr/secrets.yml"
  [ "$(wc -l < "$ENGINE_DIR/.secrets/configarr/secrets.yml")" -eq 2 ]
}

@test "update installs the tools the engine it runs pins, before bringing the stack up" {
  run update
  [ "$status" -eq 0 ]
  [ "$(grep -n '^fake-pinned-tools' "$STUB_LOG" | cut -d: -f1)" -lt "$(grep -n 'docker compose pull' "$STUB_LOG" | cut -d: -f1)" ]
}

@test "update writes the installation's makefile" {
  root=$(mktemp -d)
  mkdir -p "$root/engine/installation" "$root/config"
  cp -R "$BATS_TEST_DIRNAME/../installation/." "$root/engine/installation/"
  cp -R "$CONFIG_DIR/." "$root/config/"
  CONFIG_DIR="$root/config" ENGINE_DIR="$root/engine" run "$BATS_TEST_DIRNAME/../scripts/update.sh"
  [ -f "$root/Makefile" ]
  rm -rf "$root"
}

pin_homepage() {
  printf 'services:\n  homepage:\n    image: ghcr.io/gethomepage/homepage:v2.4.0@sha256:abc\n' > "$CONFIG_DIR/images.yml"
  mkdir -p "$ENGINE_DIR/homepage"
  cp "$BATS_TEST_DIRNAME"/../homepage/*.yaml "$ENGINE_DIR/homepage/"
  mkdir -p "$CONFIG_DIR/secrets" && : > "$CONFIG_DIR/secrets/healthchecks.sops.env"
}

@test "gluetun's control server gets a key that is made once and kept" {
  run update
  [ "$status" -eq 0 ]
  key=$(cat "$DATA_DIR/volumes/.wiring/gluetun-control.key")
  [[ $key =~ ^[0-9a-f]{32}$ ]]
  [ "$(file_mode "$DATA_DIR/volumes/.wiring/gluetun-control.key")" = 600 ]
  grep -qx "HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={\"auth\":\"apikey\",\"apikey\":\"$key\"}" "$ENGINE_DIR/.secrets/gluetun.env"
  run update
  [ "$(cat "$DATA_DIR/volumes/.wiring/gluetun-control.key")" = "$key" ]
}

@test "the landing page answers to this machine's name, plus any names the config adds" {
  MEDIA_SERVER_HOST=media.local run update
  grep -qx "HOMEPAGE_PORT=80" "$ENGINE_DIR/.env"
  grep -qx "HOMEPAGE_ALLOWED_HOSTS=media.local,localhost,127.0.0.1" "$ENGINE_DIR/.env"
  echo "HOMEPAGE_ALLOWED_HOSTS=media.tailnet.ts.net" >> "$CONFIG_DIR/installation.env"
  MEDIA_SERVER_HOST=media.local run update
  grep -qx "HOMEPAGE_ALLOWED_HOSTS=media.local,localhost,127.0.0.1,media.tailnet.ts.net" "$ENGINE_DIR/.env"
}

@test "update renders the landing page and its secrets before bringing the stack up" {
  pin_homepage
  run update
  [ "$status" -eq 0 ]
  grep -q 'title: "testinst"' "$ENGINE_DIR/.homepage/settings.yaml"
  grep -q "Sonarr:" "$ENGINE_DIR/.homepage/services.yaml"
  grep -qx "HOMEPAGE_VAR_SONARR_KEY=s1" "$ENGINE_DIR/.secrets/homepage.env"
  [ "$(file_mode "$ENGINE_DIR/.secrets/homepage.env")" = 600 ]
  [ "$(file_mode "$ENGINE_DIR/.secrets/healthchecks.env")" = 600 ]
}

@test "a key the wiring creates reaches the landing page, which restarts only then" {
  pin_homepage
  make_stub fake-wire 'mkdir -p "$DATA_DIR/volumes/.wiring"; echo new-jellyfin-key > "$DATA_DIR/volumes/.wiring/jellyfin.key"'
  run update
  [ "$status" -eq 0 ]
  grep -qx "HOMEPAGE_VAR_JELLYFIN_KEY=new-jellyfin-key" "$ENGINE_DIR/.secrets/homepage.env"
  [ "$(grep -n 'docker compose up -d homepage' "$STUB_LOG" | cut -d: -f1)" -gt "$(grep -n '^fake-wire' "$STUB_LOG" | cut -d: -f1)" ]
  : > "$STUB_LOG"
  run update
  ! grep -q 'docker compose up -d homepage' "$STUB_LOG" || false
}

@test "a failed wiring still refreshes the landing page's keys, then fails the update" {
  pin_homepage
  make_stub fake-wire 'mkdir -p "$DATA_DIR/volumes/.wiring"; echo k2 > "$DATA_DIR/volumes/.wiring/jellyfin.key"; exit 1'
  run update
  [ "$status" -ne 0 ]
  grep -qx "HOMEPAGE_VAR_JELLYFIN_KEY=k2" "$ENGINE_DIR/.secrets/homepage.env"
  ! grep -q "^fake-prune" "$STUB_LOG" || false
}

@test "the landing page can use another port than 80, and answers to its address with that port" {
  echo "HOMEPAGE_PORT=8080" >> "$CONFIG_DIR/installation.env"
  MEDIA_SERVER_HOST=media.local run update
  grep -qx "HOMEPAGE_PORT=8080" "$ENGINE_DIR/.env"
  grep -qx "HOMEPAGE_ALLOWED_HOSTS=media.local:8080,localhost:8080,127.0.0.1:8080" "$ENGINE_DIR/.env"
}

@test "the landing page links the engine version to its release on github" {
  pin_homepage
  FAKE_ENGINE_TAG=v1.0.0 run update
  [ "$status" -eq 0 ]
  grep -q "href: https://github.com/someone/media-server-engine/releases/tag/v1.0.0" "$ENGINE_DIR/.homepage/widgets.yaml"
}

@test "between releases the landing page links the engine version to its commit" {
  pin_homepage
  FAKE_ENGINE_TAG=v1.0.0-3-gabc1234 FAKE_ENGINE_REMOTE=https://github.com/someone/media-server-engine.git run update
  [ "$status" -eq 0 ]
  grep -q "href: https://github.com/someone/media-server-engine/commit/abc123" "$ENGINE_DIR/.homepage/widgets.yaml"
}

@test "a pull refused as too many requests is tried again and the update carries on" {
  FAKE_PULL_ERROR="toomanyrequests: retry-after: 896.394µs, allowed: 44000/minute" FAKE_PULL_REFUSALS=2 UPDATE_PULL_RETRY_SECONDS=0 run update
  [ "$status" -eq 0 ]
  [ "$(grep -c "^pull profiles=wiring$" "$STUB_LOG")" -eq 3 ]
  echo "$output" | grep -q "toomanyrequests"
  echo "$output" | grep -q "A registry is limiting requests; trying the pull again"
  grep -q "docker compose up -d --remove-orphans" "$STUB_LOG"
  grep -q "fake-wire" "$STUB_LOG"
}

@test "update gives up when a registry keeps refusing pulls as too many requests" {
  FAKE_PULL_ERROR="toomanyrequests: retry-after: 1s" UPDATE_PULL_ATTEMPTS=3 UPDATE_PULL_RETRY_SECONDS=0 run update
  [ "$status" -ne 0 ]
  [ "$(grep -c "^pull profiles=wiring$" "$STUB_LOG")" -eq 3 ]
  echo "$output" | grep -q "a registry kept refusing pulls as too many requests; run make update again later"
  ! grep -q "docker compose up -d --remove-orphans" "$STUB_LOG" || false
}

@test "a pull that fails for another reason is not tried again" {
  FAKE_PULL_ERROR="manifest unknown" UPDATE_PULL_RETRY_SECONDS=0 run update
  [ "$status" -ne 0 ]
  [ "$(grep -c "^pull profiles=wiring$" "$STUB_LOG")" -eq 1 ]
  echo "$output" | grep -q "manifest unknown"
  ! grep -q "docker compose up -d --remove-orphans" "$STUB_LOG" || false
}

@test "a pull refused with a bare 429 status is tried again" {
  FAKE_PULL_ERROR="unexpected status from HEAD request to https://ghcr.io/v2/x/manifests/1: 429 Too Many Requests" FAKE_PULL_REFUSALS=1 UPDATE_PULL_RETRY_SECONDS=0 run update
  [ "$status" -eq 0 ]
  [ "$(grep -c "^pull profiles=wiring$" "$STUB_LOG")" -eq 2 ]
}

@test "the main sets up its backup, verify and update checks before pulling images" {
  touch "$DATA_DIR/.backup-main"
  run update
  [ "$status" -eq 0 ]
  grep -qx "fake-healthchecks backup=testinst-backup verify=testinst-verify update=testinst-update" "$STUB_LOG"
  [ "$(line_of fake-healthchecks)" -lt "$(line_of "pull profiles")" ]
}

@test "a machine that is not the main sets up only its own update check" {
  run update
  [ "$status" -eq 0 ]
  grep -qx "fake-healthchecks update=testinst-update-$(hostname -s)" "$STUB_LOG"
}

@test "the checks tell how to reach this machine and its installation" {
  echo MEDIA_SERVER_HOST=media.example >> "$CONFIG_DIR/installation.env"
  run update
  [ "$status" -eq 0 ]
  grep -qx "healthchecks-env ssh=$(id -un)@media.example dir=$(dirname "$ENGINE_DIR")" "$STUB_LOG"
}

@test "an installation under the home directory is shown with a tilde" {
  root=$HOME/testinst
  mkdir -p "$root/engine/installation" "$root/config"
  cp -R "$BATS_TEST_DIRNAME/../installation/." "$root/engine/installation/"
  cp -R "$CONFIG_DIR/." "$root/config/"
  CONFIG_DIR="$root/config" ENGINE_DIR="$root/engine" run update
  [ "$status" -eq 0 ]
  grep -qx "healthchecks-env ssh=.* dir=~/testinst" "$STUB_LOG"
}

@test "failing to set up the checks does not stop the update" {
  FAKE_HEALTHCHECKS_FAILS=1 run update
  [ "$status" -eq 0 ]
  [[ $output == *"could not set up the healthchecks.io checks"* ]]
  grep -q "^fake-prune" "$STUB_LOG"
}
