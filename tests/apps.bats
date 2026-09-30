load helpers

setup() {
  setup_stubs
  printf 'INSTALLATION_NAME=testinst\nJELLYFIN_ADMIN_USER=admin\n' > "$CONFIG_DIR/installation.env"
  mkdir -p "$CONFIG_DIR/secrets"
  make_stub hostname 'echo homeserver'
  make_stub uname 'echo Linux'
  make_stub scutil 'echo bonjour-name'
  make_stub sops 'printf "JELLYFIN_ADMIN_PASSWORD=jelly pass\nDELUGE_WEB_PASSWORD=deluge-pass\nPORTAINER_ADMIN_PASSWORD=portainer-pass\nSONARR_API_KEY=k\n"'
}

teardown() {
  teardown_stubs
}

apps() {
  "$BATS_TEST_DIRNAME/../scripts/apps.sh" "$@"
}

@test "urls lists every app at this machine's local network name" {
  run apps urls
  [ "$status" -eq 0 ]
  for pair in Jellyfin:8096 Seerr:5055 Sonarr:8989 Radarr:7878 Prowlarr:9696 Bazarr:6767 Deluge:8112 Maintainerr:6246 Portainer:9000; do
    echo "$output" | grep -qE "^${pair%%:*} +http://homeserver\.local:${pair#*:}$" || { echo "missing $pair"; false; }
  done
}

@test "every listed port is published by the engine's compose file" {
  run apps urls
  for port in $(echo "$output" | sed -nE 's#.*:([0-9]+)$#\1#p'); do
    grep -q "\"$port:" "$BATS_TEST_DIRNAME/../docker-compose.yml" || { echo "port $port not published"; false; }
  done
}

@test "the host name can be overridden" {
  MEDIA_SERVER_HOST=192.168.1.20 run apps urls
  echo "$output" | grep -q "http://192.168.1.20:8096"
}

@test "logins show each app's user and password from the secrets, and no api keys" {
  run apps logins
  [ "$status" -eq 0 ]
  echo "$output" | grep -qE "^Jellyfin .*admin .*jelly pass"
  echo "$output" | grep -qE "^Portainer .*admin .*portainer-pass"
  echo "$output" | grep -qE "^Deluge .*deluge-pass"
  echo "$output" | grep -qE "^Seerr .*Jellyfin account"
  echo "$output" | grep -qE "^Sonarr .*no login on the local network"
  ! echo "$output" | grep -qw k || false
}

@test "an unknown command explains the usage" {
  run apps other
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "urls|logins"
}

@test "on macOS the addresses use the name the Mac announces on the network" {
  make_stub uname 'echo Darwin'
  run apps urls
  echo "$output" | grep -q "http://bonjour-name.local:8096"
}
