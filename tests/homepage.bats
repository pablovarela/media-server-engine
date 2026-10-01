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
  yaml_of "$OUT/settings.yaml" | grep -q '"title": "testinst"'
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

@test "the default page itself is reached at the landing page's port, the apps at theirs" {
  HOMEPAGE_PORT=8080 homepage render "$OUT"
  grep -q "href: http://media.local:8989" "$OUT/services.yaml"
}

@test "the default page has no bookmarks, so Homepage's sample links are not added" {
  homepage render "$OUT"
  [ "$(python3 -c 'import sys, yaml; print(yaml.safe_load(open(sys.argv[1])))' "$OUT/bookmarks.yaml")" = "[]" ]
}

@test "recently added authenticates the way current Jellyfin accepts" {
  homepage render "$OUT"
  python3 -c '
import sys, yaml
groups = yaml.safe_load(open(sys.argv[1]))
watch = next(g["Watch"] for g in groups if "Watch" in g)
widget = next(s["Recently added"]["widget"] for s in watch if "Recently added" in s)
assert widget["headers"] == {"Authorization": "MediaBrowser Token=\"{{HOMEPAGE_VAR_JELLYFIN_KEY}}\""}, widget["headers"]' "$OUT/services.yaml"
}

config_file() {
  mkdir -p "$CONFIG_DIR/homepage"
  local name=$1
  shift
  printf '%s\n' "$@" > "$CONFIG_DIR/homepage/$name"
}

setting() {
  python3 -c 'import sys, yaml; print(yaml.safe_load(open(sys.argv[1])).get(sys.argv[2]))' "$OUT/settings.yaml" "$1"
}

service_groups() {
  python3 -c 'import sys, yaml; print("|".join(next(iter(g)) for g in yaml.safe_load(open(sys.argv[1]))))' "$OUT/services.yaml"
}

group_tiles() {
  python3 -c '
import sys, yaml
groups = {next(iter(g)): g[next(iter(g))] for g in yaml.safe_load(open(sys.argv[1]))}
print("|".join(next(iter(s)) for s in groups.get(sys.argv[2], [])))' "$OUT/services.yaml" "$1"
}

calendar_views() {
  python3 -c '
import sys, yaml
groups = {next(iter(g)): g[next(iter(g))] for g in yaml.safe_load(open(sys.argv[1]))}
views = [name + "=" + s[name]["widget"]["view"] for s in groups.get(sys.argv[2], []) for name in s if (s[name].get("widget") or {}).get("type") == "calendar"]
print(" ".join(views))' "$OUT/services.yaml" "$1"
}

@test "what is coming up leads the page: an agenda next to a month's calendar" {
  homepage render "$OUT"
  [ "$(service_groups | cut -d'|' -f1-2)" = "Coming up|Watch" ]
  [ "$(calendar_views "Coming up")" = "Next up=agenda Calendar=monthly" ]
  [ -z "$(calendar_views Library)" ]
}

@test "the engine version links to its release" {
  HOMEPAGE_ENGINE_URL=https://github.com/someone/media-server-engine/releases/tag/v9.9.9 homepage render "$OUT"
  yaml_of "$OUT/widgets.yaml" | grep -q '"href": "https://github.com/someone/media-server-engine/releases/tag/v9.9.9"'
}

@test "without a known engine address, the version is plain text" {
  homepage render "$OUT"
  ! grep -q "href" "$OUT/widgets.yaml" || false
}

@test "a config holding a copy of the engine's page renders the same page" {
  homepage render "$OUT"
  for file in settings.yaml services.yaml widgets.yaml bookmarks.yaml; do cp "$OUT/$file" "$OUT/$file.engine"; done
  mkdir -p "$CONFIG_DIR/homepage"
  cp "$BATS_TEST_DIRNAME"/../homepage/*.yaml "$CONFIG_DIR/homepage/"
  homepage render "$OUT"
  for file in settings.yaml services.yaml widgets.yaml bookmarks.yaml; do cmp -s "$OUT/$file" "$OUT/$file.engine" || { echo "$file changed"; false; }; done
}
@test "the render script draws the page from the installation's settings" {
  printf 'INSTALLATION_NAME=testinst\nMEDIA_SERVER_HOST=media.local\n' > "$CONFIG_DIR/installation.env"
  make_stub git 'case "$*" in *describe*) echo v1.2.3 ;; *"remote get-url"*) echo git@github.com:someone/media-server-engine.git ;; esac'
  run "$BATS_TEST_DIRNAME/../scripts/homepage-render.sh"
  [ "$status" -eq 0 ]
  yaml_of "$ENGINE_DIR/.homepage/widgets.yaml" | grep -q '"href": "https://github.com/someone/media-server-engine/releases/tag/v1.2.3"'
  grep -q "href: http://media.local:8989" "$ENGINE_DIR/.homepage/services.yaml"
}

@test "the config's files can use the same placeholders as the engine's" {
  config_file services.yaml "- Home:" "    - Router:" "        href: http://@HOST@:8443"
  config_file widgets.yaml "- greeting:" "    text: \"@INSTALLATION_NAME@ on @HOST@\""
  homepage render "$OUT"
  grep -q "href: http://media.local:8443" "$OUT/services.yaml"
  grep -q "testinst on media.local" "$OUT/widgets.yaml"
}

@test "backup status stays off the page without an api key, even when the config declares it" {
  cp "$BATS_TEST_DIRNAME/../homepage/services.yaml" "$CONFIG_DIR/homepage-services.yaml"
  mkdir -p "$CONFIG_DIR/homepage" && mv "$CONFIG_DIR/homepage-services.yaml" "$CONFIG_DIR/homepage/services.yaml"
  homepage render "$OUT"
  ! grep -q "type: healthchecks" "$OUT/services.yaml" || false
}

@test "a file in the config is the page's file, exactly as written" {
  config_file settings.yaml "title: mine" "theme: light"
  config_file services.yaml "- Home:" "    - Router:" "        href: http://192.168.1.1"
  config_file widgets.yaml "- datetime:" "    text_size: xl"
  config_file bookmarks.yaml "- Links:" "    - Docs:" "        - href: https://gethomepage.dev"
  config_file custom.css "html { font-size: 20px; }"
  homepage render "$OUT"
  [ "$(setting title) $(setting theme)" = "mine light" ]
  [ "$(service_groups)" = Home ]
  [ "$(python3 -c 'import sys, yaml; print([next(iter(w)) for w in yaml.safe_load(open(sys.argv[1]))])' "$OUT/widgets.yaml")" = "['datetime']" ]
  grep -q "gethomepage.dev" "$OUT/bookmarks.yaml"
  [ "$(cat "$OUT/custom.css")" = "html { font-size: 20px; }" ]
}

@test "a tile commented out in the config is not on the page" {
  config_file services.yaml "- Coming up:" "    - Next up:" "        icon: mdi-calendar-clock" "#    - Calendar:" "#        icon: mdi-calendar-month"
  homepage render "$OUT"
  [ "$(group_tiles "Coming up")" = "Next up" ]
}

@test "a file the config does not have comes from the engine" {
  config_file settings.yaml "title: mine"
  homepage render "$OUT"
  [ "$(service_groups | cut -d'|' -f1)" = "Coming up" ]
  [ "$(cat "$OUT/custom.css")" = "html { font-size: 18px; }" ]
}

@test "backup status stays off the page without an api key, also inside a nested group" {
  config_file services.yaml "- Manage:" "    - Maintenance:" "        - Portainer:" "            href: http://@HOST@:9000" "        - Healthchecks:" "            widget:" "              type: healthchecks"
  homepage render "$OUT"
  ! grep -q "Healthchecks" "$OUT/services.yaml" || false
  grep -q "Portainer" "$OUT/services.yaml"
}
