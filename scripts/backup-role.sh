#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

MACHINE_ID_FILE=${MACHINE_ID_FILE:-/etc/machine-id}

machine_id() {
  local generated="$DATA_DIR/.machine-id"
  if [ -s "$MACHINE_ID_FILE" ]; then
    cat "$MACHINE_ID_FILE"
  else
    mkdir -p "$DATA_DIR"
    [ -s "$generated" ] || openssl rand -hex 16 > "$generated"
    cat "$generated"
  fi
}

readonly RESTIC_REPOSITORY_DOES_NOT_EXIST=10

latest_snapshot_field() {
  local snapshots errors status=0
  errors=$(mktemp)
  snapshots=$(restic snapshots --no-lock --host "$INSTALLATION_NAME" --latest 1 --json 2>"$errors") || status=$?
  [ "$status" -eq "$RESTIC_REPOSITORY_DOES_NOT_EXIST" ] || cat "$errors" >&2
  rm -f "$errors"
  [ "$status" -ne "$RESTIC_REPOSITORY_DOES_NOT_EXIST" ] || return 0
  [ "$status" -eq 0 ] || return "$status"
  echo "$snapshots" | python3 -c '
import json, sys
snapshots = sorted(json.load(sys.stdin) or [], key=lambda s: s["time"])
if not snapshots:
    sys.exit()
latest = snapshots[-1]
tags = dict(t.split(":", 1) for t in latest.get("tags", []) if ":" in t)
machine = tags.get("machine", "")
if sys.argv[1] == "machine":
    print(machine)
else:
    name = tags.get("machine-name") or "machine " + machine
    print(name + ", last backup " + latest["time"][:16].replace("T", " "))
' "$1"
}

main_machine() {
  latest_snapshot_field machine
}

is_main() {
  local main
  main=$(main_machine) || exit 2
  [ -z "$main" ] || [ "$main" = "$(machine_id)" ]
}

case ${1:-} in
  machine-id) machine_id ;;
  main-machine) main_machine ;;
  describe-main) latest_snapshot_field description ;;
  is-main) is_main ;;
  *) die "usage: $(basename "$0") is-main|main-machine|describe-main|machine-id" ;;
esac
