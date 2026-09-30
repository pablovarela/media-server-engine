#!/usr/bin/env bash
set -uo pipefail

failed=""
for step in "$(dirname "$0")"/[0-9][0-9]-*.sh; do
  [ -e "$step" ] || continue
  "$step" || failed="$failed $(basename "$step")"
done
if [ -n "$failed" ]; then
  echo "wiring failed:$failed" >&2
  exit 1
fi
