#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

CHECK_TOOLS_COMMAND=${CHECK_TOOLS_COMMAND:-$SCRIPTS_DIR/check-tools.sh}
RESTORE_COMMAND=${RESTORE_COMMAND:-$SCRIPTS_DIR/restore.sh}
UPDATE_COMMAND=${UPDATE_COMMAND:-$SCRIPTS_DIR/update.sh}
INSTALL_TIMERS_COMMAND=${INSTALL_TIMERS_COMMAND:-$SCRIPTS_DIR/install-timers.sh}
CLAIM_COMMAND=${CLAIM_COMMAND:-$SCRIPTS_DIR/claim-backup-main.sh}
BACKUP_ROLE_COMMAND=${BACKUP_ROLE_COMMAND:-$SCRIPTS_DIR/backup-role.sh}
export SOPS_AGE_KEY_FILE=${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}

load_installation

with_backup_secrets() {
  sops exec-env "$CONFIG_DIR/secrets/backup.sops.env" "$*"
}

has_backups() {
  local count
  count=$(with_backup_secrets restic snapshots --latest 1 --json 2>/dev/null |
    python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
  [ "${count:-0}" -gt 0 ]
}

wants_to_be_main() {
  local default=y answer
  if ! with_backup_secrets "$BACKUP_ROLE_COMMAND" is-main; then
    echo "another machine is $INSTALLATION_NAME's main; say yes only to take over from it" >&2
    default=n
  fi
  ask answer "Make this machine $INSTALLATION_NAME's main, the one that backs up? (y/n)" "$default"
  [ "$answer" = y ] || [ "$answer" = Y ]
}

"$CHECK_TOOLS_COMMAND"
if [ -n "${RESTORE_FROM_BACKUP:-}" ] && has_backups; then
  with_backup_secrets "$RESTORE_COMMAND"
fi
become_main=""
if wants_to_be_main; then become_main=1; fi
"$UPDATE_COMMAND"
if command -v systemctl >/dev/null; then
  "$INSTALL_TIMERS_COMMAND" media-update media-download-cleanup
  if [ -n "$become_main" ]; then
    sops exec-env "$CONFIG_DIR/secrets/healthchecks.sops.env" "sops exec-env '$CONFIG_DIR/secrets/backup.sops.env' '$CLAIM_COMMAND'"
    with_backup_secrets "$INSTALL_TIMERS_COMMAND" media-backup media-verify
  fi
else
  echo "This machine has no systemd, so no timers were installed; run make update by hand to apply config changes."
  if [ -n "$become_main" ]; then
    sops exec-env "$CONFIG_DIR/secrets/healthchecks.sops.env" "sops exec-env '$CONFIG_DIR/secrets/backup.sops.env' '$CLAIM_COMMAND'"
  fi
fi
echo "$(hostname -s) is set up for $INSTALLATION_NAME."
