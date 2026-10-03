#!/usr/bin/env bash
# An engine from before update.py re-runs this path after switching to a newer version.
exec "$(dirname "$0")/update.py" "$@"
