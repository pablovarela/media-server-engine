#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"
EXCLUDES_FILE="$SCRIPTS_DIR/backup-excludes.txt"
BACKUP_ROLE_COMMAND=${BACKUP_ROLE_COMMAND:-$SCRIPTS_DIR/backup-role.sh}
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

require_main() {
  [ -z "${CLAIM:-}" ] || return 0
  if ! "$BACKUP_ROLE_COMMAND" is-main; then
    rm -f "$DATA_DIR/.backup-main"
    die "another machine is $INSTALLATION_NAME's main; this machine does not back up (make claim-backup-main makes it the main)"
  fi
}

trap on_exit EXIT
ping_healthcheck backup /start
require_main
machine_id=$("$BACKUP_ROLE_COMMAND" machine-id)
services_to_restart=$(stack_compose ps --status running --services | tr '\n' ' ')
services_to_restart=${services_to_restart% }
stack_compose stop
restic backup --retry-lock 2h --host "$INSTALLATION_NAME" --tag "machine:$machine_id" --tag nightly --exclude-file "$EXCLUDES_FILE" volumes
start_stopped_services
restic forget --retry-lock 2h --host "$INSTALLATION_NAME" --prune --keep-daily 7 --keep-weekly 4 --keep-monthly 6
ping_healthcheck backup
