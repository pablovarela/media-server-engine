#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd)"

CHECK_TOOLS_COMMAND=${CHECK_TOOLS_COMMAND:-$SCRIPTS_DIR/check-tools.sh}
UPDATE_COMMAND=${UPDATE_COMMAND:-$SCRIPTS_DIR/update.sh}

on_exit() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    ping_healthcheck update /fail
  fi
}

load_installation
if [ -e "$DATA_DIR/.backup-main" ]; then MACHINE_ROLE=main; else MACHINE_ROLE=secondary; fi
export MACHINE_ROLE
trap on_exit EXIT
ping_healthcheck update /start
"$CHECK_TOOLS_COMMAND"
"$UPDATE_COMMAND"
ping_healthcheck update
