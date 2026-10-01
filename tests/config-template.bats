setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

keys_configure_writes_to_apps_secrets() {
  local internal
  internal=$(sed -n 's/^readonly INTERNAL_CREDENTIALS="\(.*\)"$/\1/p' "$REPO/scripts/configure.sh")
  sed -n 's/^ *write_secret secrets\/apps\.sops\.env //p' "$REPO/scripts/configure.sh" |
    sed "s/\$INTERNAL_CREDENTIALS/$internal/" | tr ' ' '\n'
}

@test "every secret the configarr template uses is one configure writes" {
  known=$(keys_configure_writes_to_apps_secrets)
  [ -n "$known" ]
  for secret in $(grep -o '!secret [A-Z_]*' "$REPO/config-template/configarr/config.yml" | cut -d' ' -f2 | sort -u); do
    echo "$known" | grep -qx "$secret" || { echo "configure does not write $secret"; false; }
  done
}

@test "the deluge client declares every field configarr checks, so it does not warn" {
  python3 -c '
import sys, yaml
yaml.SafeLoader.add_constructor("!secret", lambda loader, node: None)
config = yaml.safe_load(open(sys.argv[1]))
for app, imported in (("sonarr", "tv_imported_category"), ("radarr", "movie_imported_category")):
    fields = config[app]["main"]["download_clients"]["data"][0]["fields"]
    for name in ("url_base", imported, "download_directory", "completed_directory"):
        assert name in fields, (app, name)' "$REPO/config-template/configarr/config.yml"
}

@test "renovate looks for images in the config's images files" {
  python3 -c '
import json, re, sys
config = json.load(open(sys.argv[1]))
patterns = config["docker-compose"]["managerFilePatterns"]
for name in ("images.yml", "images.monitoring.yml"):
    assert any(re.search(p.strip("/"), name) for p in patterns), name' "$REPO/config-template/renovate.json"
}
