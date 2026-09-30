#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$MEDIA_SERVER_DIR"

CHECK_TOOLS_COMMAND=${CHECK_TOOLS_COMMAND:-$SCRIPTS_DIR/check-tools.sh}
DEPLOY_COMMAND=${DEPLOY_COMMAND:-$SCRIPTS_DIR/deploy.sh}

on_exit() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    ping_healthcheck "$HEALTHCHECK_DEPLOY_URL" /fail
  fi
}

trap on_exit EXIT
ping_healthcheck "$HEALTHCHECK_DEPLOY_URL" /start
"$CHECK_TOOLS_COMMAND"
"$DEPLOY_COMMAND"
ping_healthcheck "$HEALTHCHECK_DEPLOY_URL"
