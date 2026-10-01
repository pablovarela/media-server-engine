#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation
mkdir -p "$DATA_DIR"
cd "$DATA_DIR"

has_app_data() {
  [ -d volumes ] && find volumes -mindepth 1 -maxdepth 1 ! -name configarr | grep -q .
}

running_stack_containers() {
  docker ps -q --filter "label=com.docker.compose.project=media-server"
}

move_existing_volumes_aside() {
  local aside
  aside="volumes.before-restore-$(date +%Y%m%d-%H%M%S)"
  mv volumes "$aside"
  mkdir volumes
  echo "previous volumes/ kept in $DATA_DIR/$aside; delete it once the restore looks right"
}

running_containers=$(running_stack_containers) || die "cannot tell whether the stack is running (docker ps failed)"
[ -z "$running_containers" ] || die "the stack is running; stop it with make media-stop first"
if has_app_data; then
  [ "${1:-}" = --overwrite ] || die "volumes/ already holds app data; run with --overwrite to replace it with the latest backup"
  move_existing_volumes_aside
fi
restic unlock
host_filter=$(installation_snapshot_filter)
# shellcheck disable=SC2086
restic_explaining_locks restore "latest:$SNAPSHOT_VOLUMES_PATH" $host_filter --target "$DATA_DIR/volumes" --exclude configarr
