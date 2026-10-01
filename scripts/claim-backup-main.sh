#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKUP_COMMAND=${BACKUP_COMMAND:-$SCRIPTS_DIR/backup.sh}
BACKUP_ROLE_COMMAND=${BACKUP_ROLE_COMMAND:-$SCRIPTS_DIR/backup-role.sh}

readonly RESTIC_REPOSITORY_DOES_NOT_EXIST=10

create_repository_if_missing() {
  local status=0
  restic cat config >/dev/null 2>&1 || status=$?
  if [ "$status" -eq "$RESTIC_REPOSITORY_DOES_NOT_EXIST" ]; then
    echo "Creating the backup repository ${RESTIC_REPOSITORY:-}"
    restic init
  fi
}

confirm_taking_over() {
  local role_status=0 answer
  "$BACKUP_ROLE_COMMAND" is-main || role_status=$?
  [ "$role_status" -ne 0 ] || return 0
  [ "$role_status" -eq "$NOT_THE_MAIN" ] || die "$(explain_main_check "$role_status"); nothing was claimed"
  [ -z "${CLAIM_CONFIRMED:-}" ] || return 0
  echo "$INSTALLATION_NAME's main is $("$BACKUP_ROLE_COMMAND" describe-main). Taking over makes it refuse to back up." >&2
  ask answer "Make this machine the main instead? (y/n)" n
  [ "$answer" = y ] || [ "$answer" = Y ] || die "nothing was claimed"
}

create_repository_if_missing
confirm_taking_over
CLAIM=1 "$BACKUP_COMMAND"
mkdir -p "$DATA_DIR"
touch "$DATA_DIR/.backup-main"
echo "This machine is now $INSTALLATION_NAME's main; backups from any other machine are refused."
