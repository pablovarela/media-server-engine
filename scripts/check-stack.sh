#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"

problems_in() {
  python3 -c '
import json, os, sys
volumes = os.path.realpath(os.environ["DATA_DIR"]) + "/volumes/"
for name, service in json.load(sys.stdin)["services"].items():
    image = service.get("image", "(none)")
    if "@sha256:" not in image:
        print(f"{name}: image {image} is not pinned to a digest")
    writes_state = any(
        m.get("type") == "bind" and os.path.realpath(m.get("source", "")).startswith(volumes)
        for m in service.get("volumes", []))
    env = service.get("environment") or {}
    runs_as_1000 = service.get("user") == "1000:1000" or (env.get("PUID") == "1000" and env.get("PGID") == "1000")
    if writes_state and not runs_as_1000:
        print(f"{name}: writes app state but does not run as uid and gid 1000")
'
}

merged_config() {
  "$@" config --format json || die "the compose files cannot be merged; see the error above"
}

mkdir -p "$DATA_DIR/volumes"
stack=$(merged_config stack_compose_with_wiring)
monitoring=$(merged_config monitoring_compose)
problems=$(
  problems_in <<< "$stack"
  problems_in <<< "$monitoring"
)
if [ -n "$problems" ]; then
  echo "$problems" >&2
  die "the merged compose files are not safe to deploy"
fi
