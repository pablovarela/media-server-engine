load helpers

setup() {
  setup_stubs
  cp -R "$BATS_TEST_DIRNAME/../homepage" "$ENGINE_DIR/homepage"
  mkdir -p "$ENGINE_DIR/.secrets" "$DATA_DIR/volumes/.wiring" "$DATA_DIR/volumes/seerr/config" "$DATA_DIR/volumes/bazarr/config/config"
  printf 'SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nDELUGE_WEB_PASSWORD=d pass\nPORTAINER_ADMIN_PASSWORD=never\n' > "$ENGINE_DIR/.secrets/apps.env"
  echo jellyfin-key > "$DATA_DIR/volumes/.wiring/jellyfin.key"
  echo gluetun-key > "$DATA_DIR/volumes/.wiring/gluetun-control.key"
  printf '{"main": {"apiKey": "seerr-key"}}' > "$DATA_DIR/volumes/seerr/config/settings.json"
  printf 'auth:\n  apikey: bazarr-key\n' > "$DATA_DIR/volumes/bazarr/config/config/config.yaml"
  export INSTALLATION_NAME=testinst HOMEPAGE_HOST=media.local HOMEPAGE_ENGINE_VERSION=v9.9.9
  OUT="$ENGINE_DIR/.homepage"
  echo INSTALLATION_NAME=testinst > "$CONFIG_DIR/installation.env"
}

teardown() {
  stop_fake_app 2>/dev/null || true
  teardown_stubs
}

serve_checks() {
  printf '{"/api/v3/checks/": {"checks": [%s]}}' "$(printf '{"slug": "%s", "status": "up"},' "$@" | sed 's/,$//')" > "$STUB_DIR/checks.json"
  FAKE_APP_HEADERS='{"X-Api-Key": "hc-read"}' start_fake_app "$STUB_DIR/checks.json"
  export HEALTHCHECKS_API_URL="$FAKE_APP_URL/api/v3/checks/"
  echo "HEALTHCHECKS_API_KEY=hc-read" > "$ENGINE_DIR/.secrets/healthchecks.env"
}

health_tiles() {
  python3 -c '
import sys, yaml
groups = {next(iter(g)): g[next(iter(g))] for g in yaml.safe_load(open(sys.argv[1]))}
for tile in groups["Healthchecks"]:
    for name, options in tile.items():
        widget = options["widget"]
        print(name + "=" + widget["url"].split("slug=")[1] + ":" + widget["mappings"][0]["field"], end=" ")' "$OUT/services.yaml" | sed 's/ $//'
}

render_in_installation() {
  printf 'INSTALLATION_NAME=testinst\nHOMEPAGE_PORT=8080\n' > "$CONFIG_DIR/installation.env"
  make_stub git ''
  make_stub docker 'if [ "$1" = inspect ]; then [ -n "${FAKE_HOMEPAGE_RUNNING:-}" ] && echo true || exit 1; fi'
  make_stub curl ''
  "$BATS_TEST_DIRNAME/../scripts/homepage-render.sh"
}

@test "the page is drawn with the update check this machine pings" {
  make_stub docker 'exit 1'
  serve_checks testinst-backup testinst-update "testinst-update-$(hostname -s)"
  ENGINE_DIR=$ENGINE_DIR bash -c 'source "$1/scripts/lib.sh"; INSTALLATION_NAME=testinst HOMEPAGE_PORT=80 render_homepage' _ "$BATS_TEST_DIRNAME/.."
  [ "$(health_tiles)" = "Backup=testinst-backup:checks.0.status Update=testinst-update-$(hostname -s):checks.0.status" ]
  touch "$DATA_DIR/.backup-main"
  ENGINE_DIR=$ENGINE_DIR bash -c 'source "$1/scripts/lib.sh"; INSTALLATION_NAME=testinst HOMEPAGE_PORT=80 render_homepage' _ "$BATS_TEST_DIRNAME/.."
  [ "$(health_tiles)" = "Backup=testinst-backup:checks.0.status Update=testinst-update:checks.0.status" ]
}

@test "the render script draws the page from the installation's settings" {
  printf 'INSTALLATION_NAME=testinst\nMEDIA_SERVER_HOST=media.local\n' > "$CONFIG_DIR/installation.env"
  make_stub git 'case "$*" in *describe*) echo v1.2.3 ;; *"remote get-url"*) echo git@github.com:someone/media-server-engine.git ;; esac'
  run "$BATS_TEST_DIRNAME/../scripts/homepage-render.sh"
  [ "$status" -eq 0 ]
  grep -q 'href: https://github.com/someone/media-server-engine/releases/tag/v1.2.3' "$ENGINE_DIR/.homepage/widgets.yaml"
  grep -q "href: http://media.local:8989" "$ENGINE_DIR/.homepage/services.yaml"
}

@test "a running landing page restarts when its images change, so it serves the new ones" {
  mkdir -p "$CONFIG_DIR/homepage/images" && echo "<svg/>" > "$CONFIG_DIR/homepage/images/a.svg"
  FAKE_HOMEPAGE_RUNNING=1 run render_in_installation
  [ "$status" -eq 0 ]
  grep -q "^docker restart homepage" "$STUB_LOG"
  : > "$STUB_LOG"
  FAKE_HOMEPAGE_RUNNING=1 run render_in_installation
  ! grep -q "^docker restart" "$STUB_LOG" || false
}

@test "a running landing page rebuilds its cached page after a redraw" {
  FAKE_HOMEPAGE_RUNNING=1 run render_in_installation
  [ "$status" -eq 0 ]
  grep -q "^curl .*http://localhost:8080/api/revalidate" "$STUB_LOG"
}

@test "a landing page that is not running is only redrawn" {
  mkdir -p "$CONFIG_DIR/homepage/images" && echo "<svg/>" > "$CONFIG_DIR/homepage/images/a.svg"
  run render_in_installation
  [ "$status" -eq 0 ]
  ! grep -qE "^docker restart|^curl" "$STUB_LOG" || false
}
