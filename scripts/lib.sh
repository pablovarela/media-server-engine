# shellcheck shell=bash
MEDIA_SERVER_DIR=${MEDIA_SERVER_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}

die() {
  echo "$(basename "$0"): $*" >&2
  exit 1
}

export SNAPSHOT_VOLUMES_PATH=/volumes

ping_healthcheck() {
  local url=$1 suffix=${2:-}
  curl -fsS -m 10 --retry 3 -o /dev/null "$url$suffix" || true
}
