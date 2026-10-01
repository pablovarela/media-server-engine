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
APPS_COMMAND=${APPS_COMMAND:-$SCRIPTS_DIR/apps.sh}
export SOPS_AGE_KEY_FILE=${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}

load_installation

with_backup_secrets() {
  sops exec-env "$CONFIG_DIR/secrets/backup.sops.env" "$*"
}

has_backups() {
  local count
  count=$(with_backup_secrets restic snapshots --no-lock --latest 1 --json 2>/dev/null |
    python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
  [ "${count:-0}" -gt 0 ]
}

wants_to_be_main() {
  local default=y answer
  if ! with_backup_secrets "$BACKUP_ROLE_COMMAND" is-main; then
    echo "another machine is $INSTALLATION_NAME's main ($(with_backup_secrets "$BACKUP_ROLE_COMMAND" describe-main)); say yes only to take over from it" >&2
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
if systemd_running; then
  "$INSTALL_TIMERS_COMMAND" media-update media-download-cleanup
  if [ -n "$become_main" ]; then
    CLAIM_CONFIRMED=1 sops exec-env "$CONFIG_DIR/secrets/healthchecks.sops.env" "sops exec-env '$CONFIG_DIR/secrets/backup.sops.env' '$CLAIM_COMMAND'"
    with_backup_secrets "$INSTALL_TIMERS_COMMAND" media-backup media-verify
  fi
else
  if [ -n "$become_main" ]; then
    CLAIM_CONFIRMED=1 sops exec-env "$CONFIG_DIR/secrets/healthchecks.sops.env" "sops exec-env '$CONFIG_DIR/secrets/backup.sops.env' '$CLAIM_COMMAND'"
  fi
fi
print_summary() {
  local engine installation
  engine=$(cd "$ENGINE_DIR" && pwd)
  installation=$(dirname "$engine")
  echo
  echo "================================================================"
  echo "$INSTALLATION_NAME is ready on $(network_name)."
  echo
  echo "It lives in $installation; run make from there."
  echo
  echo "Apps (make urls lists them again):"
  "$APPS_COMMAND" urls | sed 's/^/  /'
  echo "Their logins: make logins (shows the passwords on this terminal)."
  echo
  echo "Everyday commands:"
  echo "  make configure    change settings and secrets, then make update applies them"
  echo "  make update       apply config changes and update the apps"
  echo "  make media-stop   stop the apps; make media-start starts them again"
  if [ -n "$become_main" ]; then
    echo "  make backup-now   back up now"
  fi
  echo
  if systemd_running; then
    echo "This machine updates itself daily at 05:00 and removes fake downloads every 15 minutes."
    if [ -n "$become_main" ]; then
      echo "It is the main: it backs up daily at 04:30 and checks the backups on Sundays at 05:30."
    else
      echo "Another machine backs up; this one never does."
    fi
  else
    echo "This machine has no systemd, so nothing runs on its own:"
    if [ -n "$become_main" ]; then
      echo "  run make update after config changes, and make backup-now to back up."
    else
      echo "  run make update after config changes."
      echo "Another machine backs up; this one never does."
    fi
  fi
  echo "================================================================"
  echo
  echo "Go to the installation, to run make from it:"
  echo "  cd $installation"
}

print_summary
