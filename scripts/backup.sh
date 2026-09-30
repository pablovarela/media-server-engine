#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation
EXCLUDES_FILE="$(cd "$(dirname "$0")" && pwd)/backup-excludes.txt"
cd "$DATA_DIR"

services_to_restart=""

start_stopped_services() {
  if [ -n "$services_to_restart" ]; then
    # shellcheck disable=SC2086
    stack_compose start $services_to_restart
    services_to_restart=""
  fi
}

on_exit() {
  local status=$?
  start_stopped_services || status=1
  if [ "$status" -ne 0 ]; then
    ping_healthcheck backup /fail
  fi
}

trap on_exit EXIT
ping_healthcheck backup /start
services_to_restart=$(stack_compose ps --status running --services | tr '\n' ' ')
services_to_restart=${services_to_restart% }
stack_compose stop
restic backup --retry-lock 2h --exclude-file "$EXCLUDES_FILE" --tag nightly volumes
start_stopped_services
restic forget --retry-lock 2h --prune --keep-daily 7 --keep-weekly 4 --keep-monthly 6
ping_healthcheck backup
