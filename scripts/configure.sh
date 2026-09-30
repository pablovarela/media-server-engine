#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

readonly ROTATABLE="sonarr:SONARR_API_KEY radarr:RADARR_API_KEY prowlarr:PROWLARR_API_KEY"
readonly INTERNAL_CREDENTIALS="SONARR_API_KEY RADARR_API_KEY PROWLARR_API_KEY"

rotate_variable() {
  local entry
  for entry in $ROTATABLE; do
    if [ "${entry%%:*}" = "$1" ]; then echo "${entry#*:}"; return 0; fi
  done
  return 1
}

load_dotenv() {
  local key value
  while IFS='=' read -r key value; do
    [ -n "$key" ] || continue
    printf -v "$key" '%s' "$value"
  done
}

load_current_values() {
  local file
  if [ -f installation.env ]; then load_dotenv < installation.env; fi
  for file in secrets/*.sops.env; do
    [ -f "$file" ] || continue
    load_dotenv < <(sops decrypt --output-type dotenv "$file")
  done
}

generate_internal_credentials() {
  local name
  for name in $INTERNAL_CREDENTIALS; do
    if [ -z "${!name:-}" ] || [ "$name" = "${ROTATE_VARIABLE:-}" ]; then
      printf -v "$name" '%s' "$(openssl rand -hex 16)"
    fi
  done
}

dotenv_of() {
  local name
  for name in "$@"; do
    printf '%s=%s\n' "$name" "${!name}"
  done
}

write_plain() {
  local file=$1 content
  shift
  content=$(dotenv_of "$@")
  if [ ! -f "$file" ] || [ "$(cat "$file")" != "$content" ]; then
    printf '%s\n' "$content" > "$file"
  fi
}

write_secret() {
  local file=$1 content current=""
  shift
  content=$(dotenv_of "$@")
  [ ! -f "$file" ] || current=$(sops decrypt --output-type dotenv "$file")
  if [ "$current" != "$content" ]; then
    mkdir -p "$(dirname "$file")"
    printf '%s\n' "$content" |
      sops encrypt --filename-override "$file" --input-type dotenv --output-type dotenv /dev/stdin > "$file.new"
    mv "$file.new" "$file"
  fi
}

engine_owner() {
  git -C "$ENGINE_DIR" remote get-url origin 2>/dev/null | sed -E 's#^.*github\.com[:/]##; s#/.*$##'
}

default_config_on_github() {
  if [ -n "${CONFIG_ON_GITHUB:-}" ]; then
    echo "$CONFIG_ON_GITHUB"
  elif gh auth status >/dev/null 2>&1; then
    echo y
  else
    echo n
  fi
}

current_subtitle_languages() {
  python3 -c '
import sys, yaml
try:
    languages = (yaml.safe_load(open("apps.yml")) or {}).get("bazarr", {}).get("languages")
except FileNotFoundError:
    languages = None
print(", ".join(languages or ["en"]))'
}

prompt_for_values() {
  echo "Installation: $INSTALLATION_NAME" >&2
  ask TZ "Time zone" "${TZ:-Etc/UTC}"
  ask CONFIG_ON_GITHUB "Keep this config in a private GitHub repo, so other machines can join? (y/n)" "$(default_config_on_github)"
  if [ "$CONFIG_ON_GITHUB" = y ]; then
    ask GITHUB_OWNER "GitHub owner of the config repo" "${GITHUB_OWNER:-$(engine_owner)}"
  else
    GITHUB_OWNER=${GITHUB_OWNER:-}
  fi
  ask JELLYFIN_ADMIN_USER "Jellyfin admin user" "${JELLYFIN_ADMIN_USER:-admin}"
  echo "Backup" >&2
  ask RESTIC_REPOSITORY "Restic repository" "${RESTIC_REPOSITORY:-b2:$INSTALLATION_NAME-media-server-backup:restic}"
  ask B2_ACCOUNT_ID "B2 key ID" "${B2_ACCOUNT_ID:-}"
  ask_secret B2_ACCOUNT_KEY "B2 application key" "${B2_ACCOUNT_KEY:-}"
  ask_password RESTIC_PASSWORD "Restic password" "${RESTIC_PASSWORD:-}"
  echo "VPN" >&2
  ask VPN_SERVICE_PROVIDER "VPN provider (gluetun name)" "${VPN_SERVICE_PROVIDER:-}"
  ask OPENVPN_USER "OpenVPN user" "${OPENVPN_USER:-}"
  ask_secret OPENVPN_PASSWORD "OpenVPN password" "${OPENVPN_PASSWORD:-}"
  ask SERVER_COUNTRIES "VPN server countries" "${SERVER_COUNTRIES:-}"
  echo "Healthchecks (optional)" >&2
  ask_secret HEALTHCHECKS_PING_KEY "healthchecks.io project ping key" "${HEALTHCHECKS_PING_KEY:-}"
  echo "App logins" >&2
  ask_password JELLYFIN_ADMIN_PASSWORD "Jellyfin admin password" "${JELLYFIN_ADMIN_PASSWORD:-}"
  ask_password DELUGE_WEB_PASSWORD "Deluge web password" "${DELUGE_WEB_PASSWORD:-}"
  ask_password PORTAINER_ADMIN_PASSWORD "Portainer admin password" "${PORTAINER_ADMIN_PASSWORD:-}"
  echo "Subtitles" >&2
  ask SUBTITLE_LANGUAGES "Subtitle languages (codes, comma separated)" "$(current_subtitle_languages)"
}

write_subtitle_languages() {
  SUBTITLE_LANGUAGES=$SUBTITLE_LANGUAGES python3 -c '
import os, yaml
wanted = [code.strip() for code in os.environ["SUBTITLE_LANGUAGES"].split(",") if code.strip()]
config = yaml.safe_load(open("apps.yml")) or {}
bazarr = config.setdefault("bazarr", {}) or {}
if bazarr.get("languages") != wanted:
    bazarr["languages"] = wanted
    config["bazarr"] = bazarr
    yaml.safe_dump(config, open("apps.yml", "w"), sort_keys=False)'
}

write_config() {
  write_plain installation.env INSTALLATION_NAME TZ CONFIG_ON_GITHUB GITHUB_OWNER JELLYFIN_ADMIN_USER RESTIC_REPOSITORY
  write_subtitle_languages
  write_secret secrets/backup.sops.env RESTIC_PASSWORD B2_ACCOUNT_ID B2_ACCOUNT_KEY
  write_secret secrets/vpn.sops.env VPN_SERVICE_PROVIDER OPENVPN_USER OPENVPN_PASSWORD SERVER_COUNTRIES
  write_secret secrets/healthchecks.sops.env HEALTHCHECKS_PING_KEY
  # shellcheck disable=SC2086
  write_secret secrets/apps.sops.env $INTERNAL_CREDENTIALS JELLYFIN_ADMIN_PASSWORD DELUGE_WEB_PASSWORD PORTAINER_ADMIN_PASSWORD
}

commit_changes() {
  [ -d .git ] || git init -q -b main
  git add -A
  if git diff --cached --quiet; then
    echo "No changes."
    return
  fi
  git diff --cached --stat
  git commit -q -m "Configure $INSTALLATION_NAME"
  COMMITTED=1
}

config_remote() {
  git remote get-url origin 2>/dev/null
}

publish_config() {
  local repository="$GITHUB_OWNER/media-server-config-$INSTALLATION_NAME"
  gh auth status >/dev/null 2>&1 || die "keeping the config on GitHub needs the GitHub CLI; run gh auth login, then make configure again"
  if gh repo view "$repository" >/dev/null 2>&1; then
    die "$repository already exists on GitHub; to use it, run: git -C \"$CONFIG_DIR\" remote add origin git@github.com:$repository.git && git -C \"$CONFIG_DIR\" push -u origin main"
  fi
  gh repo create "$repository" --private --source . --push
  echo "Published to https://github.com/$repository. Add it to the Renovate app so image and engine updates arrive as pull requests: https://github.com/apps/renovate"
}

apply_config_location() {
  local remote
  remote=$(config_remote || true)
  if [ "$CONFIG_ON_GITHUB" = y ]; then
    if [ -z "$remote" ]; then
      publish_config
    elif [ -n "${COMMITTED:-}" ]; then
      echo "Committed. Push with: git -C \"$CONFIG_DIR\" push"
    fi
  elif [ -n "$remote" ]; then
    git remote remove origin
    echo "This config is now local only. The repository at $remote is kept; delete it there if you no longer need it."
  fi
}

ROTATE_VARIABLE=""
if [ "${1:-}" = --rotate ]; then
  ROTATE_VARIABLE=$(rotate_variable "${2:-}") || die "can only rotate: sonarr, radarr, prowlarr"
fi

[ -f "$CONFIG_DIR/.sops.yaml" ] || die_not_an_installation
cd "$CONFIG_DIR"
[ -f images.yml ] || cp -R "$ENGINE_DIR/config-template/." .

load_current_values
INSTALLATION_NAME=${INSTALLATION_NAME:-${NAME:-}}
[ -n "$INSTALLATION_NAME" ] || die "this config has no installation name; create one with make create-installation NAME=<name>"
prompt_for_values
generate_internal_credentials
write_config
commit_changes
apply_config_location
