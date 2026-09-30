# shellcheck shell=bash
ENGINE_DIR=${ENGINE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
CONFIG_DIR=${CONFIG_DIR:-$ENGINE_DIR/../config}
DATA_DIR=${DATA_DIR:-$ENGINE_DIR/../data}
export ENGINE_DIR CONFIG_DIR DATA_DIR

export SNAPSHOT_VOLUMES_PATH=/volumes

die() {
  echo "$(basename "$0"): $*" >&2
  exit 1
}

installations_on_this_machine() {
  local config
  for config in "$HOME"/*/config/installation.env; do
    [ -f "$config" ] && [ -d "$(dirname "$(dirname "$config")")/engine" ] || continue
    dirname "$(dirname "$config")"
  done
}

die_not_an_installation() {
  local found
  found=$(installations_on_this_machine)
  {
    echo "$(basename "$0"): $ENGINE_DIR is not an installation: there is no config next to it."
    if [ -n "$found" ]; then
      echo "Installations on this machine; run make from the engine of the one you mean:"
      echo "$found" | while IFS= read -r installation; do echo "  cd $installation/engine"; done
    fi
    echo "To create one: make create-installation NAME=<name>"
  } >&2
  exit 1
}

load_installation() {
  [ -f "$CONFIG_DIR/installation.env" ] || die_not_an_installation
  set -a
  # shellcheck source=/dev/null
  source "$CONFIG_DIR/installation.env"
  set +a
  [ -n "${INSTALLATION_NAME:-}" ] || die "INSTALLATION_NAME is not set in $CONFIG_DIR/installation.env"
  export INSTALLATION_NAME
}

compose_files() {
  local base=$1 images=$2
  printf -- '-f %s -f %s ' "$ENGINE_DIR/$base" "$CONFIG_DIR/$images"
  if [ "$base" = docker-compose.yml ] && [ -f "$CONFIG_DIR/compose.override.yml" ]; then
    printf -- '-f %s ' "$CONFIG_DIR/compose.override.yml"
  fi
}

stack_compose() {
  # shellcheck disable=SC2046
  docker compose --project-name media-server --project-directory "$ENGINE_DIR" \
    --env-file "$ENGINE_DIR/.env" $(compose_files docker-compose.yml images.yml) "$@"
}

stack_compose_with_wiring() {
  COMPOSE_PROFILES=wiring stack_compose "$@"
}

monitoring_compose() {
  # shellcheck disable=SC2046
  docker compose --project-name monitoring --project-directory "$ENGINE_DIR" \
    --env-file "$ENGINE_DIR/.env" $(compose_files docker-compose.monitoring.yml images.monitoring.yml) "$@"
}

healthcheck_url() {
  local slug="$INSTALLATION_NAME-$1"
  [ -n "${HEALTHCHECKS_PING_KEY:-}" ] || return 0
  if [ "$1" = update ] && [ "${MACHINE_ROLE:-main}" = secondary ]; then
    slug="$slug-$(hostname -s)"
  fi
  echo "https://hc-ping.com/$HEALTHCHECKS_PING_KEY/$slug"
}

ping_healthcheck() {
  local url
  url=$(healthcheck_url "$1")
  if [ -z "$url" ]; then
    echo "no healthchecks ping key configured; not reporting $1${2:-}" >&2
    return 0
  fi
  curl -fsS -m 10 --retry 3 -o /dev/null "$url${2:-}?create=1" || true
}

generate_secret() {
  openssl rand -base64 "$1" | tr '+/' '-_' | tr -d '=\n'
}

mask() {
  if [ -n "$1" ]; then printf 'set, ends …%s' "${1: -3}"; else printf 'not set'; fi
}

read_answer() {
  local _prompt_typed=""
  if [ -t 0 ] && [ "${2:-}" = secret ]; then
    IFS= read -rs _prompt_typed
  elif ! IFS= read -r _prompt_typed && [ -z "$_prompt_typed" ]; then
    export PROMPT_INPUT_ENDED=1
  fi
  [ -t 0 ] && [ "${2:-}" != secret ] || echo >&2
  printf -v "$1" '%s' "$_prompt_typed"
}

ask() {
  local _prompt_answer
  printf '%s [%s]: ' "$2" "${3:-}" >&2
  read_answer _prompt_answer
  printf -v "$1" '%s' "${_prompt_answer:-${3:-}}"
}

ask_secret() {
  local _prompt_answer
  printf '%s [%s]: ' "$2" "$(mask "${3:-}")" >&2
  read_answer _prompt_answer secret
  printf -v "$1" '%s' "${_prompt_answer:-${3:-}}"
}

ask_password() {
  local _prompt_answer
  if [ -n "${3:-}" ]; then
    printf '%s [%s]: ' "$2" "$(mask "$3")" >&2
  else
    printf '%s [Enter generates one]: ' "$2" >&2
  fi
  read_answer _prompt_answer secret
  if [ -n "$_prompt_answer" ]; then
    printf -v "$1" '%s' "$_prompt_answer"
  elif [ -n "${3:-}" ]; then
    printf -v "$1" '%s' "$3"
  else
    printf -v "$1" '%s' "$(generate_secret 24)"
  fi
}

installation_snapshot_filter() {
  local count
  count=$(restic snapshots --host "$INSTALLATION_NAME" --json 2>/dev/null |
    python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
  if [ "${count:-0}" -gt 0 ]; then echo "--host $INSTALLATION_NAME"; fi
}

run_from_installation_directory() {
  local name=$1 script=$2 install_dir engine
  install_dir=${INSTALL_DIR:-$HOME/$name}
  engine="$install_dir/engine"
  if [ -d "$engine" ] && [ "$(cd "$engine" && pwd -P)" = "$(cd "$ENGINE_DIR" && pwd -P)" ]; then
    return 0
  fi
  [ ! -e "$engine" ] || die "$engine already exists; run make from $engine instead"
  local created_install_dir=""
  [ -e "$install_dir" ] || created_install_dir=$install_dir
  mkdir -p "$install_dir"
  git clone -q "$ENGINE_DIR" "$engine"
  git -C "$engine" checkout -q "$(git -C "$ENGINE_DIR" rev-parse HEAD)"
  git -C "$engine" remote set-url origin "$(git -C "$ENGINE_DIR" remote get-url origin)"
  echo "Installing $name in $install_dir: engine, config and data side by side." >&2
  ENGINE_DIR=$engine CONFIG_DIR=$install_dir/config DATA_DIR=$install_dir/data INSTALL_DIR=$install_dir \
    CREATED_INSTALL_DIR=$created_install_dir exec "$engine/scripts/$(basename "$script")" "$name"
}

valid_installation_name() {
  [[ $1 =~ ^[a-z0-9][a-z0-9-]{0,39}$ ]]
}

require_valid_installation_name() {
  valid_installation_name "$1" ||
    die "installation names are lowercase letters, digits and dashes, up to 40 characters, starting with a letter or digit; got '$1'"
}

network_name() {
  if [ -n "${MEDIA_SERVER_HOST:-}" ]; then
    echo "$MEDIA_SERVER_HOST"
  elif [ "$(uname -s)" = Darwin ]; then
    echo "$(scutil --get LocalHostName).local"
  else
    echo "$(hostname -s).local"
  fi
}
