#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

running=$(git -C "$ENGINE_DIR" describe --tags --always 2>/dev/null || echo unknown)
pinned=$(sed -n 's/^ENGINE_VERSION=//p' "$CONFIG_DIR/engine.env" 2>/dev/null)
echo "engine: $running"
echo "config pins: ${pinned:-nothing}"
if [ -n "$pinned" ] && [ "$pinned" != local ] && [ "$pinned" != "$running" ]; then
  echo "They differ: make update switches the engine to $pinned."
fi
