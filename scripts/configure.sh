#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
# shellcheck source=scripts/configure-fields.sh
source "$(dirname "$0")/configure-fields.sh"
# shellcheck source=scripts/configure-menus.sh
source "$(dirname "$0")/configure-menus.sh"

rotated_app_name() {
  local app=${ROTATE_APP:-}
  printf '%s%s' "$(printf '%s' "${app:0:1}" | tr '[:lower:]' '[:upper:]')" "${app:1}"
}

announce_rotation() {
  [ -n "${ROTATE_VARIABLE:-}" ] || return 0
  local message
  message="Rotating $(rotated_app_name)'s API key: a new one is generated when you save. Run make update afterwards to give it to every app that uses it."
  if use_menus; then
    dialog_value --msgbox "$message" 10 "$DIALOG_WIDTH" || true
  else
    echo "$message" >&2
  fi
}

use_menus() {
  case ${CONFIGURE_UI:-auto} in
    menus) true ;;
    prompts) false ;;
    *) [ -t 0 ] && [ -t 1 ] && command -v whiptail >/dev/null ;;
  esac
}

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
  local line
  while IFS= read -r line; do
    if [[ ${line%%=*} =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] && [ "$line" != "${line#*=}" ]; then
      printf -v "${line%%=*}" '%s' "${line#*=}"
    fi
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
  local file=$1 content current="" extra
  shift
  [ ! -f "$file" ] || current=$(cat "$file")
  content=$(dotenv_of "$@")
  extra=$(lines_not_managed "$*" "$current")
  [ -z "$extra" ] || content="$content"$'\n'"$extra"
  if [ ! -f "$file" ] || [ "$(cat "$file")" != "$content" ]; then
    printf '%s\n' "$content" > "$file"
  fi
}

lines_not_managed() {
  local managed=" $1 " line
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    [[ $managed == *" ${line%%=*} "* ]] || printf '%s\n' "$line"
  done <<< "$2"
}

write_secret() {
  local file=$1 content current="" extra
  shift
  [ ! -f "$file" ] || current=$(sops decrypt --output-type dotenv "$file")
  content=$(dotenv_of "$@")
  extra=$(lines_not_managed "$*" "$current")
  [ -z "$extra" ] || content="$content"$'\n'"$extra"
  if [ "$current" != "$content" ]; then
    mkdir -p "$(dirname "$file")"
    printf '%s\n' "$content" |
      sops encrypt --filename-override "$file" --input-type dotenv --output-type dotenv /dev/stdin > "$file.new"
    mv "$file.new" "$file"
  fi
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

write_homepage_settings() {
  HOMEPAGE_THEME=${HOMEPAGE_THEME:-} HOMEPAGE_COLOR=${HOMEPAGE_COLOR:-} HOMEPAGE_HIDDEN=${HOMEPAGE_HIDDEN:-} python3 -c '
import os, yaml
config = yaml.safe_load(open("apps.yml")) or {}
current = dict(config.get("homepage") or {})
wanted = dict(current)
for key, default in (("theme", "dark"), ("color", "slate")):
    value = os.environ["HOMEPAGE_" + key.upper()]
    if value and value != current.get(key, default):
        wanted[key] = value
hidden = [name.strip() for name in os.environ["HOMEPAGE_HIDDEN"].split(",") if name.strip()]
if hidden != (current.get("hidden") or []):
    if hidden:
        wanted["hidden"] = hidden
    else:
        wanted.pop("hidden", None)
if wanted != current:
    config["homepage"] = wanted
    yaml.safe_dump(config, open("apps.yml", "w"), sort_keys=False)'
}

write_config() {
  write_plain installation.env INSTALLATION_NAME TZ CONFIG_LOCATION GITHUB_OWNER JELLYFIN_ADMIN_USER RESTIC_REPOSITORY
  write_subtitle_languages
  [ -n "${NEW_INSTALLATION:-}" ] || write_homepage_settings
  write_secret secrets/backup.sops.env RESTIC_PASSWORD B2_ACCOUNT_ID B2_ACCOUNT_KEY
  write_secret secrets/vpn.sops.env VPN_SERVICE_PROVIDER OPENVPN_USER OPENVPN_PASSWORD SERVER_COUNTRIES
  write_secret secrets/healthchecks.sops.env HEALTHCHECKS_PING_KEY HEALTHCHECKS_API_KEY
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
  if ! gh auth status >/dev/null 2>&1; then
    echo "Keeping the config on GitHub needs the GitHub CLI logged in: run gh auth login." >&2
    return 1
  fi
  if gh repo view "$repository" >/dev/null 2>&1; then
    echo "$repository already exists on GitHub. If it is this config's, empty or not, use it with: git -C \"$CONFIG_DIR\" remote add origin git@github.com:$repository.git && git -C \"$CONFIG_DIR\" push -u origin main" >&2
    return 1
  fi
  gh repo create "$repository" --private --source . --push || return 1
  echo "Published to https://github.com/$repository. Add it to the Renovate app so image and engine updates arrive as pull requests, and add the engine repository too if it is private: https://github.com/apps/renovate"
}

config_not_published() {
  echo "The settings are saved and committed in $CONFIG_DIR, but not on GitHub yet. Fix the above, then run make configure again to publish them." >&2
  [ -n "${CONFIGURE_FROM_CREATE:-}" ] || exit 1
}

apply_config_location() {
  local remote
  remote=$(config_remote || true)
  if [ "$CONFIG_LOCATION" = github ]; then
    if [ -z "$remote" ]; then
      publish_config || config_not_published
    elif [ -n "${COMMITTED:-}" ]; then
      echo "Committed. Push with: git -C \"$CONFIG_DIR\" push"
      echo "Then run make update to apply it here; the other machines apply it at their next update."
    fi
  elif [ -n "${COMMITTED:-}" ] && [ -z "${CONFIGURE_FROM_CREATE:-}" ]; then
    echo "Committed. Run make update to apply it."
  fi
  if [ "$CONFIG_LOCATION" != github ] && [ -n "$remote" ]; then
    git remote remove origin
    echo "This config is now local only. The repository at $remote is kept; delete it there if you no longer need it."
  fi
}

ROTATE_VARIABLE=""
if [ "${1:-}" = --rotate ]; then
  ROTATE_VARIABLE=$(rotate_variable "${2:-}") || die "can only rotate: sonarr, radarr, prowlarr"
  ROTATE_APP=$2
fi

[ -f "$CONFIG_DIR/.sops.yaml" ] || die_not_an_installation
cd "$CONFIG_DIR"
[ -f images.yml ] || cp -R "$ENGINE_DIR/config-template/." .

[ -f installation.env ] || NEW_INSTALLATION=1
load_current_values
split_restic_repository
INSTALLATION_NAME=${INSTALLATION_NAME:-${NAME:-}}
[ -n "$INSTALLATION_NAME" ] || die "this config has no installation name; create one with make create-installation NAME=<name>"
announce_rotation
if use_menus; then menu_for_values; else prompt_for_values; fi
join_restic_repository
generate_internal_credentials
write_config
commit_changes
apply_config_location
