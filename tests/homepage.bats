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

@test "the config's settings are applied over the engine's, key by key" {
  config_file settings.yaml "theme: light" "color: sky" "headerStyle: boxed"
  homepage render "$OUT"
  [ "$(setting theme) $(setting color) $(setting headerStyle)" = "light sky boxed" ]
  [ "$(setting statusStyle)" = dot ]
}

@test "groups the config lays out come first, in its order, before the engine's" {
  config_file settings.yaml "layout:" "  - Downloads:" "      style: row" "      columns: 4"
  homepage render "$OUT"
  python3 -c '
import sys, yaml
layout = yaml.safe_load(open(sys.argv[1]))["layout"]
names = [next(iter(entry)) for entry in layout]
assert names[0] == "Downloads" and names[1] == "Coming up", names
assert layout[0]["Downloads"] == {"style": "row", "columns": 4}, layout[0]
assert names.count("Downloads") == 1' "$OUT/settings.yaml"
}

@test "a tile the config declares changes the engine's tile of that name" {
  config_file services.yaml "- Library:" "    - Sonarr:" "        description: TV shows"
  homepage render "$OUT"
  python3 -c '
import sys, yaml
groups = {next(iter(g)): g[next(iter(g))] for g in yaml.safe_load(open(sys.argv[1]))}
sonarr = next(s["Sonarr"] for s in groups["Library"] if "Sonarr" in s)
assert sonarr["description"] == "TV shows" and sonarr["widget"]["type"] == "sonarr", sonarr' "$OUT/services.yaml"
}

@test "a tile declared as null is left off the page" {
  config_file services.yaml "- Maintenance:" "    - Portainer: null"
  homepage render "$OUT"
  [ "$(group_tiles Maintenance)" = "Maintainerr" ]
}

@test "new tiles and groups from the config are added" {
  config_file services.yaml "- Watch:" "    - YouTube:" "        href: https://youtube.com" "- Home:" "    - Router:" "        href: http://192.168.1.1"
  homepage render "$OUT"
  [ "$(group_tiles Watch | tr '|' '\n' | tail -1)" = YouTube ]
  [ "$(group_tiles Home)" = Router ]
}

@test "the config's top bar widgets replace the engine's" {
  config_file widgets.yaml "- datetime:" "    text_size: xl"
  homepage render "$OUT"
  [ "$(python3 -c 'import sys, yaml; print([next(iter(w)) for w in yaml.safe_load(open(sys.argv[1]))])' "$OUT/widgets.yaml")" = "['datetime']" ]
}

@test "a config file that holds only comments changes nothing" {
  config_file widgets.yaml "# - datetime:"
  config_file services.yaml "# - Home: []"
  homepage render "$OUT"
  grep -q "greeting" "$OUT/widgets.yaml"
  [ "$(service_groups | cut -d'|' -f1)" = "Coming up" ]
}

@test "the config's bookmarks are used" {
  config_file bookmarks.yaml "- Links:" "    - Docs:" "        - href: https://gethomepage.dev"
  homepage render "$OUT"
  grep -q "gethomepage.dev" "$OUT/bookmarks.yaml"
}

@test "the text is 18px by default, and the config's css comes after the engine's" {
  homepage render "$OUT"
  [ "$(cat "$OUT/custom.css")" = "html { font-size: 18px; }" ]
  config_file custom.css "html { font-size: 20px; }"
  homepage render "$OUT"
  [ "$(tail -1 "$OUT/custom.css")" = "html { font-size: 20px; }" ]
  [ "$(head -1 "$OUT/custom.css")" = "html { font-size: 18px; }" ]
}

@test "the engine version links to its release" {
  HOMEPAGE_ENGINE_URL=https://github.com/someone/media-server-engine/releases/tag/v9.9.9 homepage render "$OUT"
  yaml_of "$OUT/widgets.yaml" | grep -q '"href": "https://github.com/someone/media-server-engine/releases/tag/v9.9.9"'
}

@test "without a known engine address, the version is plain text" {
  homepage render "$OUT"
  ! grep -q "href" "$OUT/widgets.yaml" || false
}

@test "the template's example files change nothing until they are uncommented" {
  homepage render "$OUT"
  for file in settings.yaml services.yaml widgets.yaml bookmarks.yaml; do cp "$OUT/$file" "$OUT/$file.engine"; done
  mkdir -p "$CONFIG_DIR/homepage"
  cp "$BATS_TEST_DIRNAME"/../config-template/homepage/* "$CONFIG_DIR/homepage/"
  homepage render "$OUT"
  for file in settings.yaml services.yaml widgets.yaml bookmarks.yaml; do cmp -s "$OUT/$file" "$OUT/$file.engine" || { echo "$file changed"; false; }; done
}
