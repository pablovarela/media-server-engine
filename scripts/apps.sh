#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

readonly APPS="Jellyfin:8096 Seerr:5055 Sonarr:8989 Radarr:7878 Prowlarr:9696 Bazarr:6767 Deluge:8112 Maintainerr:6246 Portainer:9000"

urls() {
  local app
  if [[ ,$(optional_services_pinned), == *,homepage,* ]]; then
    if [ "$(homepage_port)" = 80 ]; then
      printf '%-12s http://%s\n' Home "$(network_name)"
    else
      printf '%-12s http://%s:%s\n' Home "$(network_name)" "$(homepage_port)"
    fi
  fi
  for app in $APPS; do
    printf '%-12s http://%s:%s\n' "${app%%:*}" "$(network_name)" "${app#*:}"
  done
}

app_secret() {
  sops decrypt --output-type dotenv "$CONFIG_DIR/secrets/apps.sops.env" | sed -n "s/^$1=//p"
}

logins() {
  printf '%-12s %-10s %s\n' App User Password
  printf '%-12s %-10s %s\n' Jellyfin "$JELLYFIN_ADMIN_USER" "$(app_secret JELLYFIN_ADMIN_PASSWORD)"
  printf '%-12s %s\n' Seerr "sign in with the Jellyfin account"
  printf '%-12s %-10s %s\n' Deluge "" "$(app_secret DELUGE_WEB_PASSWORD)"
  printf '%-12s %-10s %s\n' Portainer admin "$(app_secret PORTAINER_ADMIN_PASSWORD)"
  printf '%-12s %s\n' Sonarr "no login on the local network"
  printf '%-12s %s\n' Radarr "no login on the local network"
  printf '%-12s %s\n' Prowlarr "no login on the local network"
}

case ${1:-} in
  urls) urls ;;
  logins) logins ;;
  *) die "usage: $(basename "$0") urls|logins" ;;
esac
