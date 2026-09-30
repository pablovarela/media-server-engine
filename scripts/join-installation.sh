#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

BOOTSTRAP_COMMAND=${BOOTSTRAP_COMMAND:-$SCRIPTS_DIR/bootstrap.sh}
DEPLOY_KEYS_COMMAND=${DEPLOY_KEYS_COMMAND:-$SCRIPTS_DIR/deploy-keys.sh}
CHECK_TOOLS_COMMAND=${CHECK_TOOLS_COMMAND:-$SCRIPTS_DIR/check-tools.sh}
RESTORE_COMMAND=${RESTORE_COMMAND:-$SCRIPTS_DIR/restore.sh}
UPDATE_COMMAND=${UPDATE_COMMAND:-$SCRIPTS_DIR/update.sh}
INSTALL_TIMERS_COMMAND=${INSTALL_TIMERS_COMMAND:-$SCRIPTS_DIR/install-timers.sh}
CLAIM_COMMAND=${CLAIM_COMMAND:-$SCRIPTS_DIR/claim-backup-main.sh}
BACKUP_ROLE_COMMAND=${BACKUP_ROLE_COMMAND:-$SCRIPTS_DIR/backup-role.sh}
export SOPS_AGE_KEY_FILE=${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}

NAME=${1:-${NAME:-}}
[ -n "$NAME" ] || die "usage: make join-installation NAME=<installation name>"

engine_remote_path() {
  git -C "$ENGINE_DIR" remote get-url origin | sed -E 's#^.*github\.com[:/]##; s#\.git$##'
}

ENGINE_REPO=$(engine_remote_path)
OWNER=${GITHUB_OWNER:-${ENGINE_REPO%%/*}}
CONFIG_REPO="$OWNER/media-server-config-$NAME"

with_backup_secrets() {
  sops exec-env "$CONFIG_DIR/secrets/backup.sops.env" "$*"
}

secrets_key_works() {
  sops decrypt "$CONFIG_DIR/secrets/vpn.sops.env" >/dev/null 2>&1
}

ensure_secrets_key() {
  local key
  secrets_key_works && return 0
  printf 'Paste the secrets key for %s (the AGE-SECRET-KEY-... line): ' "$NAME" >&2
  read_answer key secret
  mkdir -p "$(dirname "$SOPS_AGE_KEY_FILE")"
  chmod 700 "$(dirname "$SOPS_AGE_KEY_FILE")"
  ( umask 077; printf '%s\n' "$key" >> "$SOPS_AGE_KEY_FILE" )
  chmod 600 "$SOPS_AGE_KEY_FILE"
  secrets_key_works || die "that key cannot decrypt $CONFIG_REPO; check the password manager entry"
}

has_backups() {
  local count
  count=$(with_backup_secrets restic snapshots --latest 1 --json |
    python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
  [ "${count:-0}" -gt 0 ]
}

wants_to_be_main() {
  local default=y answer
  if ! with_backup_secrets "$BACKUP_ROLE_COMMAND" is-main; then
    echo "another machine is $NAME's main; say yes only to take over from it" >&2
    default=n
  fi
  ask answer "Make this machine $NAME's main, the one that backs up? (y/n)" "$default"
  [ "$answer" = y ] || [ "$answer" = Y ]
}

"$BOOTSTRAP_COMMAND"
"$DEPLOY_KEYS_COMMAND" "$ENGINE_REPO" "$CONFIG_REPO"
[ -d "$CONFIG_DIR/.git" ] || git clone "github-media-server-config-$NAME:$CONFIG_REPO.git" "$CONFIG_DIR"
load_installation
ensure_secrets_key
"$CHECK_TOOLS_COMMAND"
if [ -z "${SKIP_RESTORE:-}" ] && has_backups; then
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
fi
echo "$(hostname -s) has joined $NAME."
