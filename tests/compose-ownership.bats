setup() {
  REPO="$BATS_TEST_DIRNAME/.."
  WORK=$(mktemp -d)
  cp "$REPO/docker-compose.yml" "$WORK/"
  mkdir -p "$WORK/.secrets"
  : > "$WORK/.secrets/vpn.env"
}

teardown() {
  rm -rf "$WORK"
}

services_writing_volumes_as_root() {
  (cd "$WORK" && DOCKER_GID=0 docker compose config --format json) | python3 -c '
import json, sys
config = json.load(sys.stdin)
for name, service in config["services"].items():
    writes_volumes = any(
        mount.get("type") == "bind" and "/volumes/" in mount.get("source", "")
        for mount in service.get("volumes", [])
    )
    if not writes_volumes:
        continue
    env = service.get("environment") or {}
    runs_as_1000 = service.get("user") == "1000:1000" or (env.get("PUID") == "1000" and env.get("PGID") == "1000")
    if not runs_as_1000:
        print(name)
'
}

@test "every service that writes to volumes/ runs as uid and gid 1000" {
  run services_writing_volumes_as_root
  [ "$status" -eq 0 ]
  [ -z "$output" ] || { echo "running as root: $output"; false; }
}

@test "every image is pinned to a digest" {
  run grep -hE '^\s+image:' "$REPO/docker-compose.yml" "$REPO/docker-compose.monitoring.yml"
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -v '@sha256:'
}

@test "configarr waits until sonarr and radarr are healthy" {
  run bash -c "cd '$WORK' && DOCKER_GID=0 docker compose config --format json | python3 -c '
import json, sys
services = json.load(sys.stdin)[\"services\"]
waits = services[\"configarr\"].get(\"depends_on\", {})
for app in (\"sonarr\", \"radarr\"):
    assert waits.get(app, {}).get(\"condition\") == \"service_healthy\", app + \" not awaited\"
    assert \"healthcheck\" in services[app], app + \" has no healthcheck\"
'"
  [ "$status" -eq 0 ] || { echo "$output"; false; }
}
