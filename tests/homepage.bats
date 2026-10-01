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
  teardown_stubs
}

homepage() {
  python3 "$BATS_TEST_DIRNAME/../scripts/homepage.py" "$@"
}

yaml_of() {
  python3 -c 'import json, sys, yaml; print(json.dumps(yaml.safe_load(open(sys.argv[1]))))' "$1"
}

@test "the default page names the installation and the engine version, and links to this machine" {
  homepage render "$OUT"
  grep -qx 'title: "testinst"' "$OUT/settings.yaml"
  yaml_of "$OUT/widgets.yaml" | grep -q '"text": "testinst"'
  yaml_of "$OUT/widgets.yaml" | grep -q '"text": "engine v9.9.9"'
  grep -q "href: http://media.local:8989" "$OUT/services.yaml"
  ! grep -q "@[A-Z_]*@" "$OUT"/*.yaml || false
}

@test "backup status is shown only with a read-only healthchecks api key" {
  homepage render "$OUT"
  ! grep -q "type: healthchecks" "$OUT/services.yaml" || false
  echo "HEALTHCHECKS_API_KEY=hc-read" > "$ENGINE_DIR/.secrets/healthchecks.env"
  homepage render "$OUT"
  grep -q "type: healthchecks" "$OUT/services.yaml"
}

@test "a config with its own homepage files is used as it is" {
  mkdir -p "$CONFIG_DIR/homepage"
  echo "title: mine" > "$CONFIG_DIR/homepage/settings.yaml"
  echo "- Mine: []" > "$CONFIG_DIR/homepage/services.yaml"
  homepage render "$OUT"
  [ "$(cat "$OUT/settings.yaml")" = "title: mine" ]
  [ "$(cat "$OUT/services.yaml")" = "- Mine: []" ]
  [ ! -e "$OUT/widgets.yaml" ]
}

@test "the page's environment holds the keys its widgets use, and nothing else" {
  echo "HEALTHCHECKS_API_KEY=hc-read" > "$ENGINE_DIR/.secrets/healthchecks.env"
  run homepage env
  [ "$status" -eq 0 ]
  [ "$output" = "$(printf '%s\n' \
    HOMEPAGE_VAR_SONARR_KEY=s1 HOMEPAGE_VAR_RADARR_KEY=r1 HOMEPAGE_VAR_PROWLARR_KEY=p1 \
    "HOMEPAGE_VAR_DELUGE_PASSWORD=d pass" HOMEPAGE_VAR_JELLYFIN_KEY=jellyfin-key HOMEPAGE_VAR_SEERR_KEY=seerr-key \
    HOMEPAGE_VAR_BAZARR_KEY=bazarr-key HOMEPAGE_VAR_GLUETUN_KEY=gluetun-key HOMEPAGE_VAR_HEALTHCHECKS_KEY=hc-read)" ]
}

@test "keys that do not exist yet, before the first wiring, are left empty" {
  rm "$DATA_DIR/volumes/.wiring/jellyfin.key" "$DATA_DIR/volumes/seerr/config/settings.json"
  run homepage env
  [ "$status" -eq 0 ]
  echo "$output" | grep -qx "HOMEPAGE_VAR_JELLYFIN_KEY="
  echo "$output" | grep -qx "HOMEPAGE_VAR_SEERR_KEY="
}

@test "customizing copies the page in use into the config, once" {
  homepage render "$OUT"
  run "$BATS_TEST_DIRNAME/../scripts/homepage-customize.sh"
  [ "$status" -eq 0 ]
  cmp -s "$OUT/services.yaml" "$CONFIG_DIR/homepage/services.yaml"
  echo "$output" | grep -q "git -C $CONFIG_DIR"
  run "$BATS_TEST_DIRNAME/../scripts/homepage-customize.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "already"
}

@test "the default page itself is reached at the landing page's port, the apps at theirs" {
  HOMEPAGE_PORT=8080 homepage render "$OUT"
  grep -q "href: http://media.local:8989" "$OUT/services.yaml"
}
