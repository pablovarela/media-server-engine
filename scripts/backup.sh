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
LOCK=$(backup_lock)
readonly LOCK

take_backup_lock() {
  exec 9>>"$LOCK"
  lock_file_descriptor 9 || die "a backup is already running (process $(running_backup_pid))"
  echo $$ > "$LOCK"
}

release_backup_lock() {
  exec 9>&-
}

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
  release_backup_lock
  if [ "$status" -ne 0 ]; then
    ping_healthcheck backup /fail
  fi
}

require_main() {
  local status=0
  [ -z "${CLAIM:-}" ] || return 0
  "$BACKUP_ROLE_COMMAND" is-main || status=$?
  [ "$status" -ne 0 ] || return 0
  if [ "$status" -eq "$NOT_THE_MAIN" ]; then
    rm -f "$DATA_DIR/.backup-main"
    die "$(explain_main_check "$status"); this machine does not back up (make claim-backup-main makes it the main)"
  fi
  die "$(explain_main_check "$status"); nothing was backed up"
}

take_backup_lock
trap on_exit EXIT
ping_healthcheck backup /start
require_main
machine_id=$("$BACKUP_ROLE_COMMAND" machine-id)
services_to_restart=$(stack_compose ps --status running --services | tr '\n' ' ')
services_to_restart=${services_to_restart% }
restic unlock
stack_compose stop
restic_explaining_locks backup --retry-lock 2h --host "$INSTALLATION_NAME" --tag "machine:$machine_id" --tag nightly --exclude-file "$EXCLUDES_FILE" volumes
start_stopped_services
restic_explaining_locks forget --retry-lock 2h --host "$INSTALLATION_NAME" --prune --keep-daily 7 --keep-weekly 4 --keep-monthly 6
touch "$DATA_DIR/.backup-main"
ping_healthcheck backup
