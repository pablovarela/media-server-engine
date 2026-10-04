#!/usr/bin/env bash
# Timers installed before verify-backup.py run this path.
exec "$(dirname "$0")/verify-backup.py" "$@"
