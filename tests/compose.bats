setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  WORK=$(mktemp -d)
  export ENGINE_DIR="$WORK/engine" CONFIG_DIR="$WORK/config" DATA_DIR="$WORK/data"
  mkdir -p "$ENGINE_DIR/.secrets" "$CONFIG_DIR" "$DATA_DIR"
  cp "$REPO/docker-compose.yml" "$REPO/docker-compose.monitoring.yml" "$ENGINE_DIR/"
  cp -R "$REPO/scripts" "$ENGINE_DIR/"
  cp "$REPO"/config-template/images*.yml "$CONFIG_DIR/"
  : > "$ENGINE_DIR/.secrets/vpn.env"
  for file in sonarr.env radarr.env prowlarr.env portainer_admin; do : > "$ENGINE_DIR/.secrets/$file"; done
  echo "DOCKER_GID=0" > "$ENGINE_DIR/.env"
}

teardown() {
  rm -rf "$WORK"
}

merged() {
  bash -c "source '$ENGINE_DIR/scripts/lib.sh' && $1 config --format json"
}

@test "configarr only runs as a wiring step, not with the stack" {
  run merged stack_compose
  echo "$output" | python3 -c '
import json, sys
assert "configarr" not in json.load(sys.stdin)["services"]'
  run merged stack_compose_with_wiring
  echo "$output" | python3 -c '
import json, sys
assert json.load(sys.stdin)["services"]["configarr"]["profiles"] == ["wiring"]'
}

@test "the engine compose files carry no image versions" {
  ! grep -qE '^\s+image:' "$REPO/docker-compose.yml" "$REPO/docker-compose.monitoring.yml" || false
}

@test "the config template pins every service of both stacks to a digest" {
  run merged stack_compose_with_wiring
  [ "$status" -eq 0 ]
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
unpinned = [n for n, s in services.items() if "@sha256:" not in s.get("image", "")]
assert not unpinned, unpinned'
  run merged monitoring_compose
  [ "$status" -eq 0 ]
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
unpinned = [n for n, s in services.items() if "@sha256:" not in s.get("image", "")]
assert not unpinned, unpinned'
}

@test "app state, media and downloads live under DATA_DIR" {
  run merged stack_compose
  echo "$output" | python3 -c '
import json, os, sys
data = os.environ["DATA_DIR"]
outside = [(n, m["source"]) for n, s in json.load(sys.stdin)["services"].items()
           for m in s.get("volumes", []) if m.get("type") == "bind"
           and any(part in m["source"] for part in ("/volumes/", "/media/", "/downloads"))
           and not m["source"].startswith(data)]
assert not outside, outside'
}

@test "every service that writes app state runs as uid and gid 1000" {
  run merged stack_compose
  echo "$output" | python3 -c '
import json, os, sys
volumes = os.environ["DATA_DIR"] + "/volumes/"
bad = []
for name, service in json.load(sys.stdin)["services"].items():
    if not any(m.get("type") == "bind" and m.get("source", "").startswith(volumes) for m in service.get("volumes", [])):
        continue
    env = service.get("environment") or {}
    if not (service.get("user") == "1000:1000" or (env.get("PUID") == "1000" and env.get("PGID") == "1000")):
        bad.append(name)
assert not bad, bad'
}

@test "configarr reads its config from the config repo, read-only" {
  run merged stack_compose_with_wiring
  echo "$output" | python3 -c '
import json, os, sys
mounts = json.load(sys.stdin)["services"]["configarr"]["volumes"]
config = [m for m in mounts if m["target"] == "/app/config"][0]
assert config["source"] == os.environ["CONFIG_DIR"] + "/configarr", config
assert config.get("read_only"), config'
}

@test "configarr waits until sonarr and radarr are healthy" {
  run merged stack_compose_with_wiring
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
waits = services["configarr"].get("depends_on", {})
for app in ("sonarr", "radarr"):
    assert waits.get(app, {}).get("condition") == "service_healthy", app
    assert "healthcheck" in services[app], app'
}

@test "sonarr, radarr and prowlarr take their api key from their own secrets file" {
  printf 'SONARR__AUTH__APIKEY=sk\n' > "$ENGINE_DIR/.secrets/sonarr.env"
  printf 'RADARR__AUTH__APIKEY=rk\n' > "$ENGINE_DIR/.secrets/radarr.env"
  printf 'PROWLARR__AUTH__APIKEY=pk\n' > "$ENGINE_DIR/.secrets/prowlarr.env"
  run merged stack_compose
  [ "$status" -eq 0 ]
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
for app, key in (("sonarr", "sk"), ("radarr", "rk"), ("prowlarr", "pk")):
    assert services[app]["environment"].get(app.upper() + "__AUTH__APIKEY") == key, app
assert "SONARR__AUTH__APIKEY" not in services["radarr"]["environment"]'
}

@test "portainer creates its admin from a read-only password file" {
  run merged stack_compose
  echo "$output" | python3 -c '
import json, os, sys
portainer = json.load(sys.stdin)["services"]["portainer"]
assert portainer["command"] == ["--admin-password-file", "/run/secrets/portainer_admin"], portainer["command"]
mount = [m for m in portainer["volumes"] if m["target"] == "/run/secrets/portainer_admin"][0]
assert mount["source"] == os.environ["ENGINE_DIR"] + "/.secrets/portainer_admin", mount
assert mount.get("read_only"), mount'
}
