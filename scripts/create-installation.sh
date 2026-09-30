#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

CONFIGURE_COMMAND=${CONFIGURE_COMMAND:-$SCRIPTS_DIR/configure.sh}
JOIN_COMMAND=${JOIN_COMMAND:-$SCRIPTS_DIR/join-installation.sh}
export SOPS_AGE_KEY_FILE=${SOPS_AGE_KEY_FILE:-$HOME/.config/sops/age/keys.txt}

NAME=${1:-${NAME:-}}
[ -n "$NAME" ] || die "usage: make create-installation NAME=<installation name>"

ENGINE_REPO=$(git -C "$ENGINE_DIR" remote get-url origin | sed -E 's#^.*github\.com[:/]##; s#\.git$##')
OWNER=${GITHUB_OWNER:-${ENGINE_REPO%%/*}}
CONFIG_REPO="$OWNER/media-server-config-$NAME"

new_secrets_key() {
  local generated public
  generated=$(mktemp)
  rm -f "$generated"
  age-keygen -o "$generated" 2>/dev/null
  public=$(sed -n 's/^# public key: //p' "$generated")
  mkdir -p "$(dirname "$SOPS_AGE_KEY_FILE")"
  chmod 700 "$(dirname "$SOPS_AGE_KEY_FILE")"
  ( umask 077; grep '^AGE-SECRET-KEY-' "$generated" >> "$SOPS_AGE_KEY_FILE" )
  chmod 600 "$SOPS_AGE_KEY_FILE"
  {
    echo "The secrets key for $NAME. Save this line in your password manager now; without it nothing in $CONFIG_REPO can be decrypted:"
    echo
    grep '^AGE-SECRET-KEY-' "$generated"
    echo
    printf 'Press Enter once it is saved. '
  } >&2
  rm -f "$generated"
  read -r _ || true
  echo "$public"
}

write_sops_config() {
  printf 'creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: %s\n' "$1" > "$CONFIG_DIR/.sops.yaml"
}

fill_template() {
  cp -R "$ENGINE_DIR/config-template/." "$CONFIG_DIR/"
  grep -rl ENGINE_REPOSITORY "$CONFIG_DIR" | while IFS= read -r file; do
    sed -i.bak "s#ENGINE_REPOSITORY#$ENGINE_REPO#g" "$file"
    rm -f "$file.bak"
  done
}

gh auth status >/dev/null 2>&1 || die "creating an installation needs the GitHub CLI; run gh auth login first"
if gh repo view "$CONFIG_REPO" >/dev/null 2>&1; then
  die "$CONFIG_REPO already exists; use make join-installation NAME=$NAME"
fi
[ ! -e "$CONFIG_DIR" ] || [ -z "$(ls -A "$CONFIG_DIR")" ] || die "$CONFIG_DIR is not empty"

mkdir -p "$CONFIG_DIR"
public_key=$(new_secrets_key)
fill_template
write_sops_config "$public_key"
git -C "$CONFIG_DIR" init -q -b main
NAME=$NAME "$CONFIGURE_COMMAND"
gh repo create "$CONFIG_REPO" --private --source "$CONFIG_DIR" --push
echo "Created $CONFIG_REPO. Add it to the Renovate app so image and engine updates arrive as pull requests: https://github.com/apps/renovate"
"$JOIN_COMMAND" "$NAME"
