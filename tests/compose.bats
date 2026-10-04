setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  WORK=$(mktemp -d)
  export ENGINE_DIR="$WORK/engine" CONFIG_DIR="$WORK/config" DATA_DIR="$WORK/data"
  mkdir -p "$ENGINE_DIR/.secrets" "$CONFIG_DIR" "$DATA_DIR"
  cp "$REPO/docker-compose.yml" "$REPO/docker-compose.monitoring.yml" "$ENGINE_DIR/"
  cp -R "$REPO/scripts" "$ENGINE_DIR/"
  cp "$REPO"/config-template/images*.yml "$CONFIG_DIR/"
  : > "$ENGINE_DIR/.secrets/vpn.env"
  for file in sonarr.env radarr.env prowlarr.env portainer_admin homepage.env gluetun.env; do : > "$ENGINE_DIR/.secrets/$file"; done
  printf 'DOCKER_GID=0\nHOMEPAGE_ALLOWED_HOSTS=media.local\n' > "$ENGINE_DIR/.env"
}

teardown() {
  rm -rf "$WORK"
}

merged() {
  case $1 in
    stack) "$ENGINE_DIR/scripts/engine-run" stack config --format json ;;
    wiring) COMPOSE_PROFILES=wiring "$ENGINE_DIR/scripts/engine-run" stack config --format json ;;
    monitoring) "$ENGINE_DIR/scripts/engine-run" monitoring config --format json ;;
  esac
}

@test "configarr only runs as a wiring step, not with the stack" {
  run merged stack
  echo "$output" | python3 -c '
import json, sys
assert "configarr" not in json.load(sys.stdin)["services"]'
  run merged wiring
  echo "$output" | python3 -c '
import json, sys
assert json.load(sys.stdin)["services"]["configarr"]["profiles"] == ["wiring"]'
}

@test "the engine compose files carry no image versions" {
  ! grep -qE '^\s+image:' "$REPO/docker-compose.yml" "$REPO/docker-compose.monitoring.yml" || false
}

@test "the config template pins every service of both stacks to a digest" {
  run merged wiring
  [ "$status" -eq 0 ]
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
unpinned = [n for n, s in services.items() if "@sha256:" not in s.get("image", "")]
assert not unpinned, unpinned'
  run merged monitoring
  [ "$status" -eq 0 ]
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
unpinned = [n for n, s in services.items() if "@sha256:" not in s.get("image", "")]
assert not unpinned, unpinned'
}

@test "app state, media and downloads live under DATA_DIR" {
  run merged stack
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
  run merged stack
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
  run merged wiring
  echo "$output" | python3 -c '
import json, os, sys
mounts = json.load(sys.stdin)["services"]["configarr"]["volumes"]
config = [m for m in mounts if m["target"] == "/app/config"][0]
assert config["source"] == os.environ["CONFIG_DIR"] + "/configarr", config
assert config.get("read_only"), config'
}

@test "configarr waits until sonarr and radarr are healthy" {
  run merged wiring
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
  run merged stack
  [ "$status" -eq 0 ]
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
for app, key in (("sonarr", "sk"), ("radarr", "rk"), ("prowlarr", "pk")):
    assert services[app]["environment"].get(app.upper() + "__AUTH__APIKEY") == key, app
assert "SONARR__AUTH__APIKEY" not in services["radarr"]["environment"]'
}

@test "portainer creates its admin from a read-only password file" {
  run merged stack
  echo "$output" | python3 -c '
import json, os, sys
portainer = json.load(sys.stdin)["services"]["portainer"]
assert portainer["command"] == ["--admin-password-file", "/run/secrets/portainer_admin"], portainer["command"]
mount = [m for m in portainer["volumes"] if m["target"] == "/run/secrets/portainer_admin"][0]
assert mount["source"] == os.environ["ENGINE_DIR"] + "/.secrets/portainer_admin", mount
assert mount.get("read_only"), mount'
}

@test "sonarr, radarr and prowlarr skip their login on the local network, with no first-visit setup" {
  run merged stack
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
for app in ("sonarr", "radarr", "prowlarr"):
    env = services[app]["environment"]
    assert env.get(app.upper() + "__AUTH__METHOD") == "Forms", app
    assert env.get(app.upper() + "__AUTH__REQUIRED") == "DisabledForLocalAddresses", app'
}

@test "every app runs in the installation's time zone, or UTC without one" {
  echo "TZ=Europe/London" >> "$ENGINE_DIR/.env"
  run merged stack
  echo "$output" | python3 -c '
import json, sys
services = json.load(sys.stdin)["services"]
wrong = {n: s["environment"].get("TZ") for n, s in services.items() if "TZ" in (s.get("environment") or {}) and s["environment"]["TZ"] != "Europe/London"}
assert not wrong, wrong
assert "TZ" in services["jellyfin"]["environment"]'
  printf 'DOCKER_GID=0\nHOMEPAGE_ALLOWED_HOSTS=media.local\n' > "$ENGINE_DIR/.env"
  run merged stack
  echo "$output" | python3 -c '
import json, sys
assert json.load(sys.stdin)["services"]["jellyfin"]["environment"]["TZ"] == "Etc/UTC"'
}

service() {
  merged stack | python3 -c "import json, sys; print(json.dumps(json.load(sys.stdin)['services'].get('$1')))"
}

@test "the landing page runs when the config pins its image, and not otherwise" {
  [ "$(service homepage)" != null ]
  sed -i.bak '/^  homepage:/,/^  [a-z]/{/^  homepage:/d;/image: ghcr.io\/gethomepage/d;}' "$CONFIG_DIR/images.yml"
  [ "$(service homepage)" = null ]
}

@test "the landing page answers on port 80, runs as uid 1000 and reads its rendered config" {
  service homepage | python3 -c '
import json, os, sys
s = json.load(sys.stdin)
assert {"published": "80", "target": 3000} in [{"published": p["published"], "target": p["target"]} for p in s["ports"]], s["ports"]
env = s["environment"]
assert env["PUID"] == "1000" and env["PGID"] == "1000"
assert env["HOMEPAGE_ALLOWED_HOSTS"] == "media.local"
assert env["LOG_TARGETS"] == "stdout"
mounts = {m["target"]: m for m in s["volumes"]}
assert mounts["/app/config"]["source"] == os.environ["ENGINE_DIR"] + "/.homepage"
assert mounts["/media"]["source"] == os.environ["DATA_DIR"] + "/media" and mounts["/media"]["read_only"]
'
}

@test "gluetun's control server takes its key from its own secrets file" {
  echo 'HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={"auth":"apikey","apikey":"k1"}' > "$ENGINE_DIR/.secrets/gluetun.env"
  service gluetun | python3 -c '
import json, sys
env = json.load(sys.stdin)["environment"]
assert env["HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE"] == "{\"auth\":\"apikey\",\"apikey\":\"k1\"}", env'
}

@test "the landing page's host port comes from .env" {
  printf 'DOCKER_GID=0\nHOMEPAGE_ALLOWED_HOSTS=media.local\nHOMEPAGE_PORT=8080\n' > "$ENGINE_DIR/.env"
  service homepage | python3 -c '
import json, sys
assert [p["published"] for p in json.load(sys.stdin)["ports"]] == ["8080"]'
}

@test "the landing page serves the config's images from its own folder" {
  service homepage | python3 -c '
import json, os, sys
mounts = {m["target"]: m for m in json.load(sys.stdin)["volumes"]}
assert mounts["/app/public/images"]["source"] == os.environ["ENGINE_DIR"] + "/.homepage-images", mounts
assert mounts["/app/public/images"]["read_only"]'
}
