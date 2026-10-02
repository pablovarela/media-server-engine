#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

BOOTSTRAP_COMMAND=${BOOTSTRAP_COMMAND:-$SCRIPTS_DIR/bootstrap.sh}
DEPLOY_KEYS_COMMAND=${DEPLOY_KEYS_COMMAND:-$SCRIPTS_DIR/deploy-keys.sh}
SETUP_MACHINE_COMMAND=${SETUP_MACHINE_COMMAND:-$SCRIPTS_DIR/setup-machine.sh}
export SOPS_AGE_KEY_FILE=${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}

NAME=${1:-${NAME:-}}
[ -n "$NAME" ] || die "usage: make join-installation NAME=<installation name>"
require_valid_installation_name "$NAME"

run_from_installation_directory "$NAME" "$0"

engine_remote_path() {
  git -C "$ENGINE_DIR" remote get-url origin | sed -E 's#^.*github\.com[:/]##; s#\.git$##'
}

ENGINE_REPO=$(engine_remote_path)
OWNER=${GITHUB_OWNER:-${ENGINE_REPO%%/*}}
CONFIG_REPO="$OWNER/media-server-config-$NAME"

secrets_key_works() {
  sops decrypt "$CONFIG_DIR/secrets/vpn.sops.env" >/dev/null 2>&1
}

ensure_secrets_key() {
  local key previous
  secrets_key_works && return 0
  printf 'Paste the secrets key for %s (the AGE-SECRET-KEY-... line): ' "$NAME" >&2
  read_answer key secret
  key=$(printf '%s' "$key" | tr -d '[:space:]')
  printf '%s\n' "$key" | age-keygen -y >/dev/null 2>&1 ||
    die "that is not an age secrets key (the line starts with AGE-SECRET-KEY-); nothing was changed"
  mkdir -p "$(dirname "$SOPS_AGE_KEY_FILE")"
  chmod 700 "$(dirname "$SOPS_AGE_KEY_FILE")"
  previous=$(mktemp)
  [ ! -f "$SOPS_AGE_KEY_FILE" ] || cp -p "$SOPS_AGE_KEY_FILE" "$previous"
  ( umask 077; printf '%s\n' "$key" >> "$SOPS_AGE_KEY_FILE" )
  chmod 600 "$SOPS_AGE_KEY_FILE"
  if ! secrets_key_works; then
    if [ -s "$previous" ]; then cat "$previous" > "$SOPS_AGE_KEY_FILE"; else rm -f "$SOPS_AGE_KEY_FILE"; fi
    rm -f "$previous"
    die "that key cannot decrypt $CONFIG_REPO; check the password manager entry; the keys file is unchanged"
  fi
  rm -f "$previous"
}

"$BOOTSTRAP_COMMAND"
"$DEPLOY_KEYS_COMMAND" "$ENGINE_REPO" "$CONFIG_REPO:write"
git -C "$ENGINE_DIR" remote set-url origin "github-${ENGINE_REPO##*/}:$ENGINE_REPO.git"
[ -d "$CONFIG_DIR/.git" ] || git clone "github-media-server-config-$NAME:$CONFIG_REPO.git" "$CONFIG_DIR"
load_installation
ensure_secrets_key
if [ -z "${SKIP_RESTORE:-}" ]; then export RESTORE_FROM_BACKUP=1; fi
"$SETUP_MACHINE_COMMAND"
