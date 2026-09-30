# shellcheck shell=bash

readonly FIELDS="Installation|TZ|text|Time zone
Installation|CONFIG_ON_GITHUB|yesno|Keep this config in a private GitHub repo, so other machines can join?
Installation|GITHUB_OWNER|text|GitHub owner of the config repo
Installation|JELLYFIN_ADMIN_USER|text|Jellyfin admin user
Backup|RESTIC_REPOSITORY|text|Backup repository
Backup|B2_ACCOUNT_ID|text|B2 key ID
Backup|B2_ACCOUNT_KEY|secret|B2 application key
Backup|RESTIC_PASSWORD|password|Restic password
VPN|VPN_SERVICE_PROVIDER|text|VPN provider (gluetun name)
VPN|OPENVPN_USER|text|OpenVPN user
VPN|OPENVPN_PASSWORD|secret|OpenVPN password
VPN|SERVER_COUNTRIES|text|VPN server countries
Healthchecks (optional)|HEALTHCHECKS_PING_KEY|secret|healthchecks.io project ping key
App logins|JELLYFIN_ADMIN_PASSWORD|password|Jellyfin admin password
App logins|DELUGE_WEB_PASSWORD|password|Deluge web password
App logins|PORTAINER_ADMIN_PASSWORD|password|Portainer admin password (at least 12 characters)
Subtitles|SUBTITLE_LANGUAGES|text|Subtitle languages (codes, comma separated)"

readonly PORTAINER_PASSWORD_MINIMUM=12

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

field_default() {
  case $1 in
    TZ) echo "${TZ:-Etc/UTC}" ;;
    CONFIG_ON_GITHUB) default_config_on_github ;;
    GITHUB_OWNER) echo "${GITHUB_OWNER:-$(engine_owner)}" ;;
    JELLYFIN_ADMIN_USER) echo "${JELLYFIN_ADMIN_USER:-admin}" ;;
    RESTIC_REPOSITORY) echo "${RESTIC_REPOSITORY:-b2:$INSTALLATION_NAME-media-server-backup:restic}" ;;
    SUBTITLE_LANGUAGES) current_subtitle_languages ;;
    *) echo "${!1:-}" ;;
  esac
}

field_applies() {
  case $1 in
    GITHUB_OWNER) [ "${CONFIG_ON_GITHUB:-}" = y ] ;;
    B2_ACCOUNT_ID | B2_ACCOUNT_KEY) [[ ${RESTIC_REPOSITORY:-} == b2:* ]] ;;
    *) true ;;
  esac
}

field_help() {
  case $1 in
    RESTIC_REPOSITORY) echo "Where restic keeps the backups: a local path such as /mnt/backup/restic, or Backblaze B2 as b2:<bucket>:<folder>." ;;
  esac
}

field_problem() {
  case $1 in
    CONFIG_ON_GITHUB) [ "$2" = y ] || [ "$2" = n ] || echo "Answer y or n." ;;
    PORTAINER_ADMIN_PASSWORD) [ "${#2}" -ge "$PORTAINER_PASSWORD_MINIMUM" ] || echo "Portainer needs at least $PORTAINER_PASSWORD_MINIMUM characters." ;;
  esac
}

keep_unasked_field() {
  printf -v "$1" '%s' "${!1:-}"
}

prompt_field() {
  local variable=$1 kind=$2 label=$3 help problem
  help=$(field_help "$variable")
  [ -z "$help" ] || echo "  $help" >&2
  while true; do
    case $kind in
      text) ask "$variable" "$label" "$(field_default "$variable")" ;;
      yesno) ask "$variable" "$label (y/n)" "$(field_default "$variable")" ;;
      secret) ask_secret "$variable" "$label" "${!variable:-}" ;;
      password) ask_password "$variable" "$label" "${!variable:-}" ;;
    esac
    problem=$(field_problem "$variable" "${!variable}")
    [ -n "$problem" ] || return 0
    echo "$problem" >&2
    printf -v "$variable" '%s' ""
  done
}

prompt_for_values() {
  local section="" field_section variable kind label
  echo "Installation: $INSTALLATION_NAME" >&2
  while IFS='|' read -r field_section variable kind label <&3; do
    if [ "$field_section" != "$section" ]; then
      section=$field_section
      [ "$section" = Installation ] || echo "$section" >&2
    fi
    if field_applies "$variable"; then
      prompt_field "$variable" "$kind" "$label"
    else
      keep_unasked_field "$variable"
    fi
  done 3<<< "$FIELDS"
}
