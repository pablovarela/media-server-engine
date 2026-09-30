#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

BACKUP_COMMAND=${BACKUP_COMMAND:-$(cd "$(dirname "$0")" && pwd)/backup.sh}

readonly RESTIC_REPOSITORY_DOES_NOT_EXIST=10

create_repository_if_missing() {
  local status=0
  restic cat config >/dev/null 2>&1 || status=$?
  if [ "$status" -eq "$RESTIC_REPOSITORY_DOES_NOT_EXIST" ]; then
    echo "Creating the backup repository ${RESTIC_REPOSITORY:-}"
    restic init
  fi
}

create_repository_if_missing
CLAIM=1 "$BACKUP_COMMAND"
mkdir -p "$DATA_DIR"
touch "$DATA_DIR/.backup-main"
echo "This machine is now $INSTALLATION_NAME's main; backups from any other machine are refused."
