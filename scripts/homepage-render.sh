#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation
render_homepage
echo "The landing page is redrawn; an open page reloads itself in a few seconds."
