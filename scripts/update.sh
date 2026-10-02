#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPT_PATH="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
SCRIPTS_DIR="$(dirname "$SCRIPT_PATH")"

CHECK_STACK_COMMAND=${CHECK_STACK_COMMAND:-$SCRIPTS_DIR/check-stack.sh}
WIRE_COMMAND=${WIRE_COMMAND:-$SCRIPTS_DIR/wire/wire_apps.py}
PRUNE_COMMAND=${PRUNE_COMMAND:-$SCRIPTS_DIR/prune-stack-images.sh}
PINNED_TOOLS_COMMAND=${PINNED_TOOLS_COMMAND:-$SCRIPTS_DIR/bootstrap.sh}
readonly GLUETUN_DEPENDENTS="prowlarr flaresolverr deluge"

config_has_remote() {
  git -C "$CONFIG_DIR" remote get-url origin >/dev/null 2>&1
}

uncommitted_changes() {
  git -C "$1" status --porcelain --untracked-files=no
}

config_changes() {
  git -C "$CONFIG_DIR" status --porcelain
}

require_clean_engine() {
  local changes
  changes=$(uncommitted_changes "$ENGINE_DIR")
  [ -z "$changes" ] || { echo "$changes" >&2; die "uncommitted changes in the engine at $ENGINE_DIR; an installation's engine is not edited, change the engine repository instead"; }
}

require_clean_config() {
  local changes
  changes=$(config_changes)
  [ -n "$changes" ] || return 0
  {
    echo "The config has changes that are not committed:"
    echo "$changes"
    echo "See them with: git -C $CONFIG_DIR diff"
    if config_has_remote; then
      echo "Commit and push them, then run make update again:"
      echo "  git -C $CONFIG_DIR commit -am \"<what changed>\" && git -C $CONFIG_DIR push"
    else
      echo "Commit them, then run make update again:"
      echo "  git -C $CONFIG_DIR commit -am \"<what changed>\""
    fi
  } >&2
  exit 1
}

pinned_engine_version() {
  sed -n 's/^ENGINE_VERSION=//p' "$CONFIG_DIR/engine.env"
}

switch_engine_and_restart_if_needed() {
  local wanted current
  wanted=$(pinned_engine_version)
  [ -n "$wanted" ] && [ "$wanted" != local ] || return 0
  current=$(git -C "$ENGINE_DIR" describe --tags --exact-match 2>/dev/null || true)
  [ "$current" != "$wanted" ] || return 0
  git -C "$ENGINE_DIR" fetch --tags --quiet
  git -C "$ENGINE_DIR" rev-parse -q --verify "refs/tags/$wanted" >/dev/null ||
    die "engine version $wanted not found; staying on ${current:-the current checkout}"
  git -C "$ENGINE_DIR" checkout -q --detach "$wanted"
  MEDIA_SERVER_PULLED=1 exec "$SCRIPT_PATH" "$@"
}

configarr_secrets_from() {
  python3 -c '
import json, re, sys
try:
    config = open(sys.argv[1]).read()
except FileNotFoundError:
    config = ""
wanted = set(re.findall(r"!secret\s+([A-Za-z0-9_]+)", config))
for line in sys.stdin:
    key, _, value = line.rstrip("\n").partition("=")
    if key in wanted:
        print(f"{key}: {json.dumps(value)}")
' "$1"
}

app_secret() {
  sed -n "s/^$1=//p" "$ENGINE_DIR/.secrets/apps.env"
}

write_app_secret_files() {
  local app upper
  for app in sonarr radarr prowlarr; do
    upper=$(echo "$app" | tr '[:lower:]' '[:upper:]')
    echo "${upper}__AUTH__APIKEY=$(app_secret "${upper}_API_KEY")" > "$ENGINE_DIR/.secrets/$app.env"
  done
  printf '%s' "$(app_secret PORTAINER_ADMIN_PASSWORD)" > "$ENGINE_DIR/.secrets/portainer_admin"
}

gluetun_control_key() {
  local key="$DATA_DIR/volumes/.wiring/gluetun-control.key"
  [ -s "$key" ] || { mkdir -p "$(dirname "$key")"; openssl rand -hex 16 > "$key"; }
  cat "$key"
}

decrypt_healthchecks_secrets() {
  local secrets="$CONFIG_DIR/secrets/healthchecks.sops.env"
  if [ -f "$secrets" ]; then
    sops decrypt --output-type dotenv "$secrets" > "$ENGINE_DIR/.secrets/healthchecks.env"
  else
    : > "$ENGINE_DIR/.secrets/healthchecks.env"
  fi
}

decrypt_secrets() {
  (
    umask 077
    mkdir -p "$ENGINE_DIR/.secrets/configarr"
    sops decrypt --output-type dotenv "$CONFIG_DIR/secrets/vpn.sops.env" > "$ENGINE_DIR/.secrets/vpn.env"
    sops decrypt --output-type dotenv "$CONFIG_DIR/secrets/apps.sops.env" > "$ENGINE_DIR/.secrets/apps.env"
    decrypt_healthchecks_secrets
    printf 'HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"%s"}\n' "$(gluetun_control_key)" > "$ENGINE_DIR/.secrets/gluetun.env"
    configarr_secrets_from "$CONFIG_DIR/configarr/config.yml" < "$ENGINE_DIR/.secrets/apps.env" > "$ENGINE_DIR/.secrets/configarr/secrets.yml"
    write_app_secret_files
  )
}

docker_socket_gid() {
  local socket=/var/run/docker.sock
  if [ "$(uname -s)" = Linux ] && [ -S "$socket" ]; then
    stat -L -c %g "$socket"
  else
    echo 0
  fi
}

homepage_allowed_hosts() {
  local port_suffix="" host hosts=""
  [ "$(homepage_port)" = 80 ] || port_suffix=":$(homepage_port)"
  local extra_hosts=${HOMEPAGE_ALLOWED_HOSTS:-}
  for host in $(network_name) localhost 127.0.0.1 ${extra_hosts//,/ }; do
    hosts="$hosts,$host$port_suffix"
  done
  echo "${hosts#,}"
}

write_compose_env() {
  printf 'DOCKER_GID=%s\nTZ=%s\nHOMEPAGE_PORT=%s\nHOMEPAGE_ALLOWED_HOSTS=%s\n' \
    "$(docker_socket_gid)" "${TZ:-Etc/UTC}" "$(homepage_port)" "$(homepage_allowed_hosts)" > "$ENGINE_DIR/.env"
}

homepage_env_changed() {
  local env="$ENGINE_DIR/.secrets/homepage.env"
  ( umask 077; python3 "$SCRIPTS_DIR/homepage.py" env > "$env.new" )
  if cmp -s "$env.new" "$env"; then
    rm -f "$env.new"
    return 1
  fi
  mv "$env.new" "$env"
}

homepage_pinned() {
  [[ ,$(optional_services_pinned), == *,homepage,* ]]
}

refresh_homepage() {
  if homepage_env_changed && homepage_pinned; then
    stack_compose up -d homepage
  fi
}

create_bind_mount_directories() {
  stack_compose_with_wiring config --format json | python3 -c '
import json, os, sys
data = os.environ["DATA_DIR"]
for service in json.load(sys.stdin)["services"].values():
    for mount in service.get("volumes", []):
        if mount.get("type") == "bind" and mount.get("source", "").startswith(data):
            print(mount["source"])
' | while IFS= read -r dir; do
    mkdir -p "$dir"
  done
}

gluetun_dependents_not_attached_to_current_gluetun() {
  local gluetun_id dependent
  gluetun_id=$(stack_compose ps -q gluetun)
  for dependent in $GLUETUN_DEPENDENTS; do
    if [ "$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$dependent" 2>/dev/null)" != "container:$gluetun_id" ]; then
      echo "$dependent"
    fi
  done
}

reattach_gluetun_dependents() {
  local detached
  detached=$(gluetun_dependents_not_attached_to_current_gluetun | tr '\n' ' ')
  if [ -n "${detached// /}" ]; then
    # shellcheck disable=SC2086
    stack_compose up -d --force-recreate --no-deps $detached
  fi
}

wait_for_running_backup() {
  local deadline=$((SECONDS + ${UPDATE_BACKUP_WAIT_SECONDS:-3600})) announced=""
  while [ -n "$(running_backup_pid)" ]; do
    [ "$SECONDS" -lt "$deadline" ] || die "a backup is still running; run make update again once it has finished"
    [ -n "$announced" ] || { echo "Waiting for the running backup to finish..."; announced=1; }
    sleep "${UPDATE_BACKUP_POLL_SECONDS:-10}"
  done
}

pull_images() {
  local attempts=${UPDATE_PULL_ATTEMPTS:-4} attempt=1 output wait_seconds
  while ! output=$(stack_compose_with_wiring pull --quiet 2>&1); do
    printf '%s\n' "$output" >&2
    case $output in *toomanyrequests* | *"Too Many Requests"*) ;; *) return 1 ;; esac
    [ "$attempt" -lt "$attempts" ] || die "a registry kept refusing pulls as too many requests; run make update again later"
    wait_seconds=$((${UPDATE_PULL_RETRY_SECONDS:-30} * attempt))
    echo "A registry is limiting requests; trying the pull again in $wait_seconds seconds..."
    sleep "$wait_seconds"
    attempt=$((attempt + 1))
  done
  [ -z "$output" ] || printf '%s\n' "$output"
}

require_clean_engine
require_clean_config
wait_for_running_backup
if [ -z "${MEDIA_SERVER_PULLED:-}" ]; then
  if config_has_remote; then git -C "$CONFIG_DIR" pull --ff-only; fi
  switch_engine_and_restart_if_needed "$@"
fi
"$PINNED_TOOLS_COMMAND" --pinned-tools
load_installation
write_installation_makefile
decrypt_secrets
write_compose_env
render_homepage
homepage_env_changed || true
"$CHECK_STACK_COMMAND"
create_bind_mount_directories
pull_images
up_status=0
stack_compose up -d --remove-orphans || up_status=$?
reattach_gluetun_dependents
[ "$up_status" -eq 0 ] || exit "$up_status"
wire_status=0
"$WIRE_COMMAND" || wire_status=$?
refresh_homepage
[ "$wire_status" -eq 0 ] || exit "$wire_status"
"$PRUNE_COMMAND"
