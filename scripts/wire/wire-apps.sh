#!/usr/bin/env bash
set -uo pipefail

WIRE_WAIT_SECONDS=${WIRE_WAIT_SECONDS:-60}
WIRE_RETRY_SECONDS=${WIRE_RETRY_SECONDS:-2}

app_of() {
  local name=${1%.sh}
  echo "${name#[0-9][0-9]-}"
}

health_url() {
  case $1 in
    prowlarr) echo "${PROWLARR_URL:-http://localhost:9696}/ping" ;;
    jellyfin) echo "${JELLYFIN_URL:-http://localhost:8096}/health" ;;
    deluge) echo "${DELUGE_URL:-http://localhost:8112}/" ;;
    seerr) echo "${SEERR_URL:-http://localhost:5055}/api/v1/status" ;;
    bazarr) echo "${BAZARR_URL:-http://localhost:6767}/api/system/ping" ;;
    maintainerr) echo "${MAINTAINERR_URL:-http://localhost:6246}/api/settings/version" ;;
  esac
}

wait_until_answering() {
  local url deadline=$((SECONDS + WIRE_WAIT_SECONDS))
  url=$(health_url "$1")
  [ -n "$url" ] || return 0
  until curl -fsS -o /dev/null --max-time 5 "$url" 2>/dev/null; do
    [ "$SECONDS" -lt "$deadline" ] || return 1
    sleep "$WIRE_RETRY_SECONDS"
  done
}

failed=""
for step in "$(dirname "$0")"/[0-9][0-9]-*.sh; do
  [ -e "$step" ] || continue
  app=$(app_of "$(basename "$step")")
  if ! wait_until_answering "$app"; then
    echo "$app is not answering after ${WIRE_WAIT_SECONDS}s; skipped its wiring" >&2
    failed="$failed $(basename "$step")"
    continue
  fi
  "$step" || failed="$failed $(basename "$step")"
done
if [ -n "$failed" ]; then
  echo "wiring failed:$failed" >&2
  exit 1
fi
