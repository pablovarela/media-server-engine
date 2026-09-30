#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

BACKUP_COMMAND=${BACKUP_COMMAND:-$(cd "$(dirname "$0")" && pwd)/backup.sh}

CLAIM=1 "$BACKUP_COMMAND"
mkdir -p "$DATA_DIR"
touch "$DATA_DIR/.backup-main"
echo "This machine is now $INSTALLATION_NAME's main; backups from any other machine are refused."
