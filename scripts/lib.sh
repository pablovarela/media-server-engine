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

load_installation() {
  if [ -f "$CONFIG_DIR/installation.env" ]; then
    set -a
    # shellcheck source=/dev/null
    source "$CONFIG_DIR/installation.env"
    set +a
  fi
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
  local typed=""
  if [ -t 0 ] && [ "${2:-}" = secret ]; then
    IFS= read -rs typed
  else
    IFS= read -r typed || true
  fi
  [ -t 0 ] && [ "${2:-}" != secret ] || echo >&2
  printf -v "$1" '%s' "$typed"
}

ask() {
  local var=$1 label=$2 default=${3:-} answer
  printf '%s [%s]: ' "$label" "$default" >&2
  read_answer answer
  printf -v "$var" '%s' "${answer:-$default}"
}

ask_secret() {
  local var=$1 label=$2 current=${3:-} answer
  printf '%s [%s]: ' "$label" "$(mask "$current")" >&2
  read_answer answer secret
  printf -v "$var" '%s' "${answer:-$current}"
}

ask_password() {
  local var=$1 label=$2 current=${3:-} answer
  if [ -n "$current" ]; then
    printf '%s [%s]: ' "$label" "$(mask "$current")" >&2
  else
    printf '%s [Enter generates one]: ' "$label" >&2
  fi
  read_answer answer secret
  if [ -n "$answer" ]; then
    printf -v "$var" '%s' "$answer"
  elif [ -n "$current" ]; then
    printf -v "$var" '%s' "$current"
  else
    printf -v "$var" '%s' "$(generate_secret 24)"
  fi
}

installation_snapshot_filter() {
  local count
  count=$(restic snapshots --host "$INSTALLATION_NAME" --json 2>/dev/null |
    python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
  if [ "${count:-0}" -gt 0 ]; then echo "--host $INSTALLATION_NAME"; fi
}
