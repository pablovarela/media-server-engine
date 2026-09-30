#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPT_PATH="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
cd "$MEDIA_SERVER_DIR"

readonly GLUETUN_DEPENDENTS="prowlarr flaresolverr deluge"

require_clean_tree() {
  local changes
  changes=$(git status --porcelain --untracked-files=no)
  if [ -n "$changes" ]; then
    echo "$changes" >&2
    die "uncommitted changes on this machine; commit them from a clone and push instead"
  fi
}

decrypt_secrets() {
  (
    umask 077
    mkdir -p .secrets volumes/configarr/config
    sops decrypt --output-type dotenv secrets/vpn.sops.env > .secrets/vpn.env
    sops decrypt volumes/configarr/config/secrets.sops.yml > volumes/configarr/config/secrets.yml
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

write_compose_env() {
  echo "DOCKER_GID=$(docker_socket_gid)" > .env
}

create_bind_mount_directories() {
  grep -oE '\./(volumes/[^: ]+|media/[^: ]+|downloads)' docker-compose.yml | sort -u | while IFS= read -r dir; do
    mkdir -p "$dir"
  done
}

pull_and_restart_if_moved() {
  local before
  before=$(git rev-parse HEAD)
  git pull --ff-only
  if [ "$(git rev-parse HEAD)" != "$before" ]; then
    MEDIA_SERVER_PULLED=1 exec "$SCRIPT_PATH" "$@"
  fi
}

gluetun_dependents_not_attached_to_current_gluetun() {
  local gluetun_id dependent
  gluetun_id=$(docker compose ps -q gluetun)
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
    docker compose up -d --force-recreate --no-deps $detached
  fi
}

require_clean_tree
[ -n "${MEDIA_SERVER_PULLED:-}" ] || pull_and_restart_if_moved "$@"
decrypt_secrets
write_compose_env
create_bind_mount_directories
docker compose pull --quiet
up_status=0
docker compose up -d --remove-orphans || up_status=$?
reattach_gluetun_dependents
[ "$up_status" -eq 0 ] || exit "$up_status"
"$(dirname "$0")/prune-stack-images.sh"
