#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

CHECK_TOOLS_COMMAND=${CHECK_TOOLS_COMMAND:-$SCRIPTS_DIR/check-tools.sh}
UPDATE_COMMAND=${UPDATE_COMMAND:-$SCRIPTS_DIR/update.sh}
PINNED_TOOLS_COMMAND=${PINNED_TOOLS_COMMAND:-$SCRIPTS_DIR/bootstrap.sh}

on_exit() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    ping_healthcheck update /fail
  fi
}

load_installation
MACHINE_ROLE=$(machine_role)
export MACHINE_ROLE
trap on_exit EXIT
ping_healthcheck update /start
"$PINNED_TOOLS_COMMAND" --pinned-tools
"$CHECK_TOOLS_COMMAND"
"$UPDATE_COMMAND"
ping_healthcheck update
