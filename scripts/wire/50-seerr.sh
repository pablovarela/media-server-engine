#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/../lib.sh"
exec python3 "$(dirname "$0")/seerr.py"
