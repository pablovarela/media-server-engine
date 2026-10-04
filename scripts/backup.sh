#!/usr/bin/env bash
# Timers installed before backup.py run this path.
exec "$(dirname "$0")/backup.py" "$@"
