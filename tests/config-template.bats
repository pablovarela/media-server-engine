setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
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

@test "a config never commits decrypted secrets, leftovers of an encryption or Finder files" {
  work=$(mktemp -d)
  cp -R "$REPO/config-template/." "$work/"
  git -C "$work" init -q
  mkdir -p "$work/secrets"
  for ignored in secrets/vpn.env secrets/apps.sops.env.new .DS_Store; do
    touch "$work/$ignored"
    git -C "$work" check-ignore -q "$ignored" || { echo "not ignored: $ignored"; false; }
  done
  touch "$work/secrets/vpn.sops.env"
  ! git -C "$work" check-ignore -q secrets/vpn.sops.env || { echo "an encrypted secret is ignored"; false; }
  rm -rf "$work"
}

@test "the template's config schema is the major of its engine version" {
  schema=$(sed -n 's/^config: *//p' "$REPO/config-template/config.yml")
  version=$(sed -n 's/^ENGINE_VERSION=v//p' "$REPO/config-template/engine.env")
  [ -n "$schema" ]
  [ "$schema" = "${version%%.*}" ]
}
