#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
cd "$MEDIA_SERVER_DIR"

has_app_data() {
  [ -d volumes ] && find volumes -mindepth 1 -maxdepth 1 ! -name configarr | grep -q .
}

compose_project_name() {
  if [ -n "${COMPOSE_PROJECT_NAME:-}" ]; then
    echo "$COMPOSE_PROJECT_NAME"
  else
    basename "$MEDIA_SERVER_DIR" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9_-'
  fi
}

running_stack_containers() {
  docker ps -q --filter "label=com.docker.compose.project=$(compose_project_name)"
}

move_existing_volumes_aside() {
  local aside
  aside="volumes.before-restore-$(date +%Y%m%d-%H%M%S)"
  mv volumes "$aside"
  mkdir volumes
  git checkout -- volumes/configarr
  echo "previous volumes/ kept in $aside; delete it once the restore looks right"
}

running_containers=$(running_stack_containers) || die "cannot tell whether the stack is running (docker ps failed)"
[ -z "$running_containers" ] || die "the stack is running; stop it with make media-stop first"
if has_app_data; then
  [ "${1:-}" = --overwrite ] || die "volumes/ already holds app data; run with --overwrite to replace it with the latest backup"
  move_existing_volumes_aside
fi
restic restore "latest:$SNAPSHOT_VOLUMES_PATH" --target "$MEDIA_SERVER_DIR/volumes" --exclude configarr
