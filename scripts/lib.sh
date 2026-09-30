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
    # shellcheck source=/dev/null
    source "$CONFIG_DIR/installation.env"
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

ping_healthcheck() {
  local url=$1 suffix=${2:-}
  curl -fsS -m 10 --retry 3 -o /dev/null "$url$suffix" || true
}
