# shellcheck shell=bash

readonly FIELDS="Installation|TZ|timezone|Time zone
Installation|CONFIG_LOCATION|choice|Where to keep this config
Installation|GITHUB_OWNER|text|GitHub owner of the config repo
Installation|JELLYFIN_ADMIN_USER|text|Jellyfin admin user
Backup|BACKUP_TYPE|choice|Where to keep the backups
Backup|BACKUP_FOLDER|text|Backup folder
Backup|BACKUP_URL|text|Restic repository
Backup|B2_BUCKET|text|B2 bucket
Backup|B2_FOLDER|text|Folder inside the bucket
Backup|B2_ACCOUNT_ID|text|B2 key ID
Backup|B2_ACCOUNT_KEY|secret|B2 application key
Backup|RESTIC_PASSWORD|password|Restic password
VPN|VPN_SERVICE_PROVIDER|text|VPN provider (gluetun name)
VPN|OPENVPN_USER|secret|OpenVPN user
VPN|OPENVPN_PASSWORD|secret|OpenVPN password
VPN|SERVER_COUNTRIES|text|VPN server countries
Healthchecks (optional)|HEALTHCHECKS_PING_KEY|secret|healthchecks.io project ping key
Healthchecks (optional)|HEALTHCHECKS_API_KEY|secret|healthchecks.io read-only API key (backup status on the landing page)
App logins|JELLYFIN_ADMIN_PASSWORD|password|Jellyfin admin password
App logins|DELUGE_WEB_PASSWORD|password|Deluge web password
App logins|PORTAINER_ADMIN_PASSWORD|password|Portainer admin password (at least 12 characters)
Subtitles|SUBTITLE_LANGUAGES|text|Subtitle languages (codes, comma separated)"

readonly PORTAINER_PASSWORD_MINIMUM=12
readonly RESTIC_REPOSITORY_DOES_NOT_EXIST=10
readonly RESTIC_WRONG_PASSWORD=12
readonly BACKUP_CHECK_SECONDS=${BACKUP_CHECK_SECONDS:-45}

current_subtitle_languages() {
  python3 -c '
import sys, yaml
try:
    languages = (yaml.safe_load(open("apps.yml")) or {}).get("bazarr", {}).get("languages")
except FileNotFoundError:
    languages = None
print(", ".join(languages or ["en"]))'
}

engine_owner() {
  git -C "$ENGINE_DIR" remote get-url origin 2>/dev/null | sed -E 's#^.*github\.com[:/]##; s#/.*$##'
}

split_restic_repository() {
  local rest
  if [[ ${RESTIC_REPOSITORY:-} == b2:* ]]; then
    BACKUP_TYPE=b2
    rest=${RESTIC_REPOSITORY#b2:}
    B2_BUCKET=${rest%%:*}
    B2_FOLDER=${rest#*:}
    [ "$B2_FOLDER" != "$rest" ] || B2_FOLDER=""
  elif [[ ${RESTIC_REPOSITORY:-} == /* ]]; then
    BACKUP_TYPE=local
    BACKUP_FOLDER=$RESTIC_REPOSITORY
  elif [ -n "${RESTIC_REPOSITORY:-}" ]; then
    BACKUP_TYPE=other
    BACKUP_URL=$RESTIC_REPOSITORY
  fi
}

join_restic_repository() {
  case $BACKUP_TYPE in
    b2) RESTIC_REPOSITORY="b2:$B2_BUCKET${B2_FOLDER:+:$B2_FOLDER}" ;;
    other) RESTIC_REPOSITORY=$BACKUP_URL ;;
    *) RESTIC_REPOSITORY=$BACKUP_FOLDER ;;
  esac
}

field_choices() {
  case $1 in
    CONFIG_LOCATION) printf '%s\n' "local|Only on this machine" "github|A private GitHub repo, so other machines can join" ;;
    BACKUP_TYPE) printf '%s\n' "local|A folder on this machine or on a mounted disk" "b2|Backblaze B2" "other|Another restic repository (sftp:, s3:, rest: ...), its credentials added to secrets/backup.sops.env by hand" ;;
  esac
}

field_default() {
  case $1 in
    TZ) echo "${TZ:-Etc/UTC}" ;;
    CONFIG_LOCATION) echo "${CONFIG_LOCATION:-local}" ;;
    GITHUB_OWNER) echo "${GITHUB_OWNER:-$(engine_owner)}" ;;
    JELLYFIN_ADMIN_USER) echo "${JELLYFIN_ADMIN_USER:-admin}" ;;
    BACKUP_TYPE) echo "${BACKUP_TYPE:-local}" ;;
    BACKUP_FOLDER) echo "${BACKUP_FOLDER:-$HOME/$INSTALLATION_NAME-backups}" ;;
    B2_BUCKET) echo "${B2_BUCKET:-$INSTALLATION_NAME-media-server-backup}" ;;
    B2_FOLDER) echo "${B2_FOLDER-restic}" ;;
    SUBTITLE_LANGUAGES) current_subtitle_languages ;;
    *) echo "${!1:-}" ;;
  esac
}

field_applies() {
  case $1 in
    GITHUB_OWNER) [ "${CONFIG_LOCATION:-}" = github ] ;;
    BACKUP_FOLDER) [ "${BACKUP_TYPE:-}" = local ] ;;
    BACKUP_URL) [ "${BACKUP_TYPE:-}" = other ] ;;
    B2_BUCKET | B2_FOLDER | B2_ACCOUNT_ID | B2_ACCOUNT_KEY) [ "${BACKUP_TYPE:-}" = b2 ] ;;
    *) true ;;
  esac
}

field_help() {
  case $1 in
    CONFIG_LOCATION) echo "You can switch at any time with make configure: switching to GitHub publishes the config, switching back to local keeps the GitHub repo." ;;
    BACKUP_FOLDER) echo "An absolute path; ~ stands for your home folder. It is created if it does not exist." ;;
    BACKUP_URL) echo "As restic takes it in -r, such as sftp:user@host:/srv/restic or s3:s3.amazonaws.com/bucket/restic. It is not checked here." ;;
    TZ) echo "A name from the time zone database, such as Europe/London or America/New_York." ;;
  esac
}

field_normalize() {
  case $1 in
    BACKUP_FOLDER)
      if [[ $2 == "~"* ]]; then printf '%s' "$HOME${2#"~"}"; else printf '%s' "$2"; fi
      ;;
    *) printf '%s' "$2" ;;
  esac
}

known_time_zone() {
  python3 -c 'import sys, zoneinfo; zoneinfo.ZoneInfo(sys.argv[1])' "$1" 2>/dev/null
}

field_problem() {
  local choices
  case $1 in
    TZ) known_time_zone "$2" || echo "$2 is not a time zone. Use a name such as Europe/London." ;;
    CONFIG_LOCATION | BACKUP_TYPE)
      choices=$(field_choices "$1" | cut -d'|' -f1 | tr '\n' ' ')
      [[ " $choices" == *" $2 "* ]] || echo "Choose one of: ${choices% }."
      ;;
    BACKUP_FOLDER) [[ $2 == /* ]] || echo "The backup folder must be an absolute path, such as /mnt/backup/restic." ;;
    BACKUP_URL) [[ $2 =~ ^[a-z0-9]+: ]] || echo "A restic repository starts with its kind, such as sftp: or s3:." ;;
    PORTAINER_ADMIN_PASSWORD) [ "${#2}" -ge "$PORTAINER_PASSWORD_MINIMUM" ] || echo "Portainer needs at least $PORTAINER_PASSWORD_MINIMUM characters." ;;
  esac
}

local_folder_problem() {
  local probe
  if mkdir -p "$BACKUP_FOLDER" 2>/dev/null && probe=$(mktemp "$BACKUP_FOLDER/.media-server-check.XXXXXX" 2>/dev/null); then
    rm -f "$probe"
  else
    echo "$BACKUP_FOLDER cannot be created or written to by $(id -un)."
  fi
}

b2_problem() {
  local output status=0
  command -v restic >/dev/null || return 0
  output=$(B2_ACCOUNT_ID=$B2_ACCOUNT_ID B2_ACCOUNT_KEY=$B2_ACCOUNT_KEY RESTIC_PASSWORD=$RESTIC_PASSWORD \
    perl -e 'alarm shift; exec @ARGV' "$BACKUP_CHECK_SECONDS" restic -r "$RESTIC_REPOSITORY" cat config 2>&1 >/dev/null) || status=$?
  case $status in
    0 | "$RESTIC_REPOSITORY_DOES_NOT_EXIST") ;;
    "$RESTIC_WRONG_PASSWORD") echo "The restic password does not open the backups already in $RESTIC_REPOSITORY." ;;
    *) echo "Backblaze B2 did not accept these details: $(printf '%s\n' "$output" | grep -v '^$' | tail -1)" ;;
  esac
}

backup_problem() {
  case ${BACKUP_TYPE:-} in
    b2) b2_problem ;;
    other) ;;
    *) local_folder_problem ;;
  esac
}

keep_unasked_field() {
  printf -v "$1" '%s' "${!1:-}"
}

prompt_field() {
  local variable=$1 kind=$2 label=$3 help problem choices
  help=$(field_help "$variable")
  [ -z "$help" ] || echo "  $help" >&2
  if [ "$kind" = choice ]; then
    field_choices "$variable" | sed 's/^\([^|]*\)|/    \1: /' >&2
    choices=$(field_choices "$variable" | cut -d'|' -f1 | paste -sd/ -)
    label="$label ($choices)"
  fi
  while true; do
    case $kind in
      text | timezone | choice) ask "$variable" "$label" "$(field_default "$variable")" ;;
      secret) ask_secret "$variable" "$label" "${!variable:-}" ;;
      password) ask_password "$variable" "$label" "${!variable:-}" ;;
    esac
    printf -v "$variable" '%s' "$(field_normalize "$variable" "${!variable}")"
    problem=$(field_problem "$variable" "${!variable}")
    [ -n "$problem" ] || return 0
    echo "$problem" >&2
    [ -z "${PROMPT_INPUT_ENDED:-}" ] || die "the answers ran out while $label was not valid"
    printf -v "$variable" '%s' ""
  done
}

prompt_keep_anyway() {
  local keep
  echo "$1" >&2
  ask keep "Keep it anyway? (y/n)" n
  [ -z "${PROMPT_INPUT_ENDED:-}" ] || die "the answers ran out before the backup details were settled"
  [ "$keep" = y ] || [ "$keep" = Y ]
}

prompt_section() {
  local wanted=$1 section variable kind label
  while IFS='|' read -r section variable kind label <&3; do
    [ "$section" = "$wanted" ] || continue
    if field_applies "$variable"; then
      prompt_field "$variable" "$kind" "$label"
    else
      keep_unasked_field "$variable"
    fi
  done 3<<< "$FIELDS"
}

sections() {
  cut -d'|' -f1 <<< "$FIELDS" | uniq
}

prompt_for_values() {
  local section problem
  echo "Installation: $INSTALLATION_NAME" >&2
  while IFS= read -r section <&4; do
    [ "$section" = Installation ] || echo "$section" >&2
    while true; do
      prompt_section "$section"
      [ "$section" = Backup ] || break
      join_restic_repository
      problem=$(backup_problem)
      [ -n "$problem" ] || break
      ! prompt_keep_anyway "$problem" || break
    done
  done 4< <(sections)
}
