#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
cd "$MEDIA_SERVER_DIR"

UNIT_DIR=${UNIT_DIR:-/etc/systemd/system}

render_unit() {
  sed -e "s|@MEDIA_SERVER_DIR@|$MEDIA_SERVER_DIR|g" \
    -e "s|@USER@|$(id -un)|g" \
    -e "s|@GROUP@|$(id -gn)|g" \
    -e "s|@HOME@|$HOME|g" \
    -e "s|@SOPS@|$(command -v sops)|g" \
    "$1"
}

[ $# -gt 0 ] || die "usage: $(basename "$0") UNIT_NAME..., for example media-backup"
command -v systemctl >/dev/null || die "timers need systemd, and this machine has no systemctl"
timers=""
for name in "$@"; do
  for unit in "systemd/$name.service" "systemd/$name.timer"; do
    render_unit "$unit" | sudo tee "$UNIT_DIR/$(basename "$unit")" >/dev/null
  done
  timers="$timers $name.timer"
done
sudo systemctl daemon-reload
# shellcheck disable=SC2086
sudo systemctl enable --now $timers
