#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

readonly ROTATABLE="sonarr:SONARR_API_KEY radarr:RADARR_API_KEY prowlarr:PROWLARR_API_KEY deluge:DELUGE_DAEMON_PASSWORD"
readonly INTERNAL_CREDENTIALS="SONARR_API_KEY RADARR_API_KEY PROWLARR_API_KEY DELUGE_DAEMON_PASSWORD"

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

prompt_for_values() {
  echo "Installation" >&2
  ask INSTALLATION_NAME "Installation name" "${NAME:-${INSTALLATION_NAME:-}}"
  ask TZ "Time zone" "${TZ:-Etc/UTC}"
  ask GITHUB_OWNER "GitHub owner of the config repo" "${GITHUB_OWNER:-}"
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
}

write_config() {
  write_plain installation.env INSTALLATION_NAME TZ GITHUB_OWNER JELLYFIN_ADMIN_USER RESTIC_REPOSITORY
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
  echo "Committed. Push with: git -C \"$CONFIG_DIR\" push"
}

ROTATE_VARIABLE=""
if [ "${1:-}" = --rotate ]; then
  ROTATE_VARIABLE=$(rotate_variable "${2:-}") || die "can only rotate: sonarr, radarr, prowlarr, deluge"
fi

mkdir -p "$CONFIG_DIR"
cd "$CONFIG_DIR"
[ -f .sops.yaml ] || die "$CONFIG_DIR has no .sops.yaml; create the installation with make create-installation"
[ -f installation.env ] || cp -R "$ENGINE_DIR/config-template/." .

load_current_values
prompt_for_values
generate_internal_credentials
write_config
commit_changes
