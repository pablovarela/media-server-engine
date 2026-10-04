#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
cd "$ENGINE_DIR"

UNIT_DIR=${UNIT_DIR:-/etc/systemd/system}

absolute_path() {
  mkdir -p "$1"
  (cd "$1" && pwd)
}

render_unit() {
  sed -e "s|@ENGINE_DIR@|$(absolute_path "$ENGINE_DIR")|g" \
    -e "s|@CONFIG_DIR@|$(absolute_path "$CONFIG_DIR")|g" \
    -e "s|@DATA_DIR@|$(absolute_path "$DATA_DIR")|g" \
    -e "s|@USER@|$(id -un)|g" \
    -e "s|@GROUP@|$(id -gn)|g" \
    -e "s|@HOME@|$HOME|g" \
    -e "s|@SOPS@|$(command -v sops)|g" \
    "$1"
}

installs_backup_timers() {
  local name
  for name in "$@"; do
    case $name in media-backup | media-verify) return 0 ;; esac
  done
  return 1
}

[ $# -gt 0 ] || die "usage: $(basename "$0") UNIT_NAME..., for example media-backup"
systemd_running || die "timers need systemd, and systemd is not running on this machine"
if installs_backup_timers "$@"; then
  load_installation
  "${ENGINE_RUN:-$ENGINE_DIR/scripts/engine-run}" backup-role is-main ||
    die "this machine is not $INSTALLATION_NAME's main; run make claim-backup-main first"
fi
timers=""
for name in "$@"; do
  for unit in "systemd/$name.service" "systemd/$name.timer"; do
    render_unit "$unit" | sudo tee "$UNIT_DIR/$(basename "$unit")" >/dev/null
  done
  timers="$timers $name.timer"
done
if installs_backup_timers "$@"; then
  mkdir -p "$DATA_DIR"
  touch "$DATA_DIR/.backup-main"
fi
sudo systemctl daemon-reload
# shellcheck disable=SC2086
sudo systemctl enable --now $timers
