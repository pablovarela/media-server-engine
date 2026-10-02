#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

POLL_SECONDS=${DEPLOY_KEY_POLL_SECONDS:-5}
SSH_DIR="$HOME/.ssh"

ensure_key() {
  local key="$SSH_DIR/$1-deploy"
  [ -f "$key" ] || ssh-keygen -q -t ed25519 -N "" -f "$key" -C "$1 on $(hostname -s)"
  echo "$key"
}

ensure_alias() {
  local repo=$1 key=$2
  touch "$SSH_DIR/config"
  chmod 600 "$SSH_DIR/config"
  grep -qx "Host github-$repo" "$SSH_DIR/config" && return 0
  printf 'Host github-%s\n  HostName github.com\n  User git\n  IdentityFile %s\n  IdentitiesOnly yes\n  StrictHostKeyChecking accept-new\n' \
    "$repo" "$key" >> "$SSH_DIR/config"
}

key_accepted() {
  local greeting
  greeting=$(ssh -T -o BatchMode=yes "github-$1" 2>&1 || true)
  case $greeting in *"successfully authenticated"*) return 0 ;; *) return 1 ;; esac
}

add_key() {
  local owner_repo=$1 repo=${1#*/} key=$2 access=$3
  if gh auth status >/dev/null 2>&1; then
    # shellcheck disable=SC2046
    gh repo deploy-key add "$key.pub" --repo "$owner_repo" --title "$(hostname -s)" $([ "$access" = write ] && echo --allow-write)
  else
    if [ "$access" = write ]; then
      echo "Add this deploy key to $owner_repo, with Allow write access ticked:"
    else
      echo "Add this read-only deploy key to $owner_repo:"
    fi
    echo "  $(cat "$key.pub")"
    echo "  https://github.com/$owner_repo/settings/keys/new"
  fi
  echo "Waiting for GitHub to accept the key for $repo (Ctrl-C to stop)..."
  until key_accepted "$repo"; do sleep "$POLL_SECONDS"; done
  echo "GitHub accepts the key for $repo."
}

[ $# -gt 0 ] || die "usage: $(basename "$0") OWNER/REPO[:write]..."
mkdir -p "$SSH_DIR"
chmod 700 "$SSH_DIR"
for wanted in "$@"; do
  owner_repo=${wanted%:write}
  access="read"
  [ "$wanted" = "$owner_repo" ] || access="write"
  repo=${owner_repo#*/}
  key=$(ensure_key "$repo")
  ensure_alias "$repo" "$key"
  key_accepted "$repo" || add_key "$owner_repo" "$key" "$access"
done
