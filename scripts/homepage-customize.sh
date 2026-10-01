#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

custom="$CONFIG_DIR/homepage"
rendered="$ENGINE_DIR/.homepage"
[ ! -e "$custom" ] || die "$custom already exists: the landing page is already customised; edit the files there"
[ -f "$rendered/services.yaml" ] || die "no landing page has been rendered yet; run make update first"
mkdir -p "$custom"
for file in settings.yaml services.yaml widgets.yaml; do
  cp "$rendered/$file" "$custom/$file"
done
echo "The landing page's files are now in $custom and are used as they are; the engine's defaults no longer apply."
echo "Edit them, then commit and run make update:"
echo "  git -C $CONFIG_DIR add homepage && git -C $CONFIG_DIR commit -m \"Customise the landing page\""
