#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

BOOTSTRAP_COMMAND=${BOOTSTRAP_COMMAND:-$SCRIPTS_DIR/bootstrap.sh}
CONFIGURE_COMMAND=${CONFIGURE_COMMAND:-$SCRIPTS_DIR/configure.sh}
SETUP_MACHINE_COMMAND=${SETUP_MACHINE_COMMAND:-$SCRIPTS_DIR/setup-machine.sh}
export SOPS_AGE_KEY_FILE=${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}

NAME=${1:-${NAME:-}}
[ -n "$NAME" ] || die "usage: make create-installation NAME=<installation name>"
require_valid_installation_name "$NAME"

run_from_installation_directory "$NAME" "$0"

ENGINE_REPO=$(git -C "$ENGINE_DIR" remote get-url origin | sed -E 's#^.*github\.com[:/]##; s#\.git$##')
OWNER=${GITHUB_OWNER:-${ENGINE_REPO%%/*}}
CONFIG_REPO="$OWNER/media-server-config-$NAME"

new_secrets_key() {
  local generated
  generated=$(mktemp)
  rm -f "$generated"
  age-keygen -o "$generated" 2>/dev/null
  public_key=$(sed -n 's/^# public key: //p' "$generated")
  mkdir -p "$(dirname "$SOPS_AGE_KEY_FILE")"
  chmod 700 "$(dirname "$SOPS_AGE_KEY_FILE")"
  ( umask 077; grep '^AGE-SECRET-KEY-' "$generated" >> "$SOPS_AGE_KEY_FILE" )
  chmod 600 "$SOPS_AGE_KEY_FILE"
  {
    echo "The secrets key for $NAME. Save this line in your password manager now; without it nothing in $NAME's config can be decrypted:"
    echo
    grep '^AGE-SECRET-KEY-' "$generated"
    echo
    printf 'Press Enter once it is saved. '
  } >&2
  rm -f "$generated"
  read -r _ || true
}

remove_secrets_key() {
  local kept line
  kept=$(mktemp "$SOPS_AGE_KEY_FILE.XXXXXX")
  while IFS= read -r line; do
    if [[ $line == AGE-SECRET-KEY-* ]] && [ "$(printf '%s\n' "$line" | age-keygen -y 2>/dev/null)" = "$1" ]; then
      continue
    fi
    printf '%s\n' "$line"
  done < "$SOPS_AGE_KEY_FILE" > "$kept"
  chmod 600 "$kept"
  mv "$kept" "$SOPS_AGE_KEY_FILE"
}

undo_unless_settings_saved() {
  local status=$?
  [ "$status" -ne 0 ] || return 0
  if [ -n "$settings_saved" ]; then
    echo "$NAME's settings are saved in $CONFIG_DIR. Finish setting up this machine with: cd $(dirname "$ENGINE_DIR") && make setup-machine" >&2
    return
  fi
  if [ -n "$public_key" ]; then
    remove_secrets_key "$public_key"
    echo "The secrets key shown for $NAME was removed and is no longer needed; delete it from your password manager." >&2
  fi
  rm -rf "$CONFIG_DIR"
  [ -z "$created_data_dir" ] || rm -rf "$DATA_DIR"
  [ -z "${CREATED_INSTALL_DIR:-}" ] || rm -rf "$CREATED_INSTALL_DIR"
  echo "Stopped before $NAME's settings were saved, so nothing was kept. Run make create-installation NAME=$NAME again." >&2
}

write_sops_config() {
  printf 'creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: %s\n' "$1" > "$CONFIG_DIR/.sops.yaml"
}

fill_template() {
  cp -R "$ENGINE_DIR/config-template/." "$CONFIG_DIR/"
  mkdir -p "$CONFIG_DIR/homepage"
  cp "$ENGINE_DIR"/homepage/* "$CONFIG_DIR/homepage/"
  grep -rl ENGINE_REPOSITORY "$CONFIG_DIR" | while IFS= read -r file; do
    sed -i.bak "s#ENGINE_REPOSITORY#$ENGINE_REPO#g" "$file"
    rm -f "$file.bak"
  done
}

pin_engine_version() {
  local version
  version=$(git -C "$ENGINE_DIR" describe --tags --exact-match 2>/dev/null || true)
  echo "ENGINE_VERSION=${version:-local}" > "$CONFIG_DIR/engine.env"
}

if gh auth status >/dev/null 2>&1 && gh repo view "$CONFIG_REPO" >/dev/null 2>&1; then
  die "$CONFIG_REPO already exists; use make join-installation NAME=$NAME"
fi
[ ! -e "$CONFIG_DIR" ] || [ -z "$(ls -A "$CONFIG_DIR")" ] || die "$CONFIG_DIR is not empty"

public_key="" settings_saved="" created_data_dir=""
[ -e "$DATA_DIR" ] || created_data_dir=1
trap undo_unless_settings_saved EXIT
trap 'exit 130' INT TERM

"$BOOTSTRAP_COMMAND"
mkdir -p "$CONFIG_DIR" "$DATA_DIR"
new_secrets_key
fill_template
pin_engine_version
write_sops_config "$public_key"
git -C "$CONFIG_DIR" init -q -b main
NAME=$NAME CONFIGURE_FROM_CREATE=1 "$CONFIGURE_COMMAND"
settings_saved=1
"$SETUP_MACHINE_COMMAND"
