load helpers

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/.secrets"
  printf 'DELUGE_WEB_PASSWORD=web pass\n' > "$ENGINE_DIR/.secrets/apps.env"
  cp "$BATS_TEST_DIRNAME/../config-template/apps.yml" "$CONFIG_DIR/apps.yml"
  DELUGE_CONFIG="$DATA_DIR/volumes/deluge/config"
  mkdir -p "$DELUGE_CONFIG/plugins"
  cp "$BATS_TEST_DIRNAME/fixtures/deluge/core.conf" "$DELUGE_CONFIG/core.conf"
  make_stub docker '
case "$*" in
  *"python3 -c"*) echo 3.12 ;;
  *"sh -c"*) [ -z "${FAKE_BUILD_FAILS:-}" ] || exit 1; touch "$DELUGE_CONFIG/plugins/AutoRemovePlus-2.0.0-py3.12.egg" ;;
esac'
  export DELUGE_CONFIG DELUGE_READY_SECONDS=2
}

teardown() {
  stop_fake_app
  teardown_stubs
}

deluge_web() {
  printf '{"effects": {"POST /json": {"respond": {"result": true, "error": null, "id": 1}}}}' > "$STUB_DIR/deluge-web.json"
  start_fake_app "$STUB_DIR/deluge-web.json"
}

wire() {
  DELUGE_URL=$FAKE_APP_URL "$BATS_TEST_DIRNAME/../scripts/wire/30-deluge.sh"
}

conf_value() {
  python3 -c '
import json, sys
text = open(sys.argv[1]).read()
decoder = json.JSONDecoder()
header, end = decoder.raw_decode(text)
body, _ = decoder.raw_decode(text[end:])
print(json.dumps(header if sys.argv[2] == "header" else body[sys.argv[2]]))' "$DELUGE_CONFIG/$1" "$2"
}

password_matches() {
  python3 -c '
import hashlib, json, sys
text = open(sys.argv[1]).read()
decoder = json.JSONDecoder()
_, end = decoder.raw_decode(text)
body, _ = decoder.raw_decode(text[end:])
digest = hashlib.sha1(body["pwd_salt"].encode()); digest.update(sys.argv[2].encode())
sys.exit(0 if digest.hexdigest() == body["pwd_sha1"] else 1)' "$DELUGE_CONFIG/web.conf" "$1"
}

wire_once() {
  deluge_web
  run wire
  stop_fake_app
  : > "$STUB_LOG"
}

@test "a fresh deluge gets its plugins, settings and web password, restarted once" {
  deluge_web
  run wire
  [ "$status" -eq 0 ]
  [ "$(conf_value core.conf enabled_plugins)" = '["Label", "AutoRemovePlus"]' ]
  [ "$(conf_value core.conf download_location)" = '"/downloads"' ]
  password_matches "web pass"
  [ "$(conf_value web.conf first_login)" = false ]
  [ "$(conf_value web.conf header)" = '{"file": 2, "format": 1}' ]
  [ "$(conf_value autoremoveplus.conf min)" = 168.0 ]
  [ "$(conf_value autoremoveplus.conf sel_func)" = '"or"' ]
  for file in web.conf autoremoveplus.conf; do
    [ "$(stat -f %Lp "$DELUGE_CONFIG/$file" 2>/dev/null || stat -c %a "$DELUGE_CONFIG/$file")" = 600 ]
  done
  [ "$(grep -c '^docker stop deluge$' "$STUB_LOG")" -eq 1 ]
  [ "$(grep -c '^docker start deluge$' "$STUB_LOG")" -eq 1 ]
  echo "$output" | grep -q "deluge: enable plugin AutoRemovePlus"
  echo "$output" | grep -q "deluge: set web password"
}

@test "a missing plugin egg is built in the container from the pinned source, before the restart" {
  deluge_web
  run wire
  build=$(grep -n 'docker exec -u abc deluge sh -c' "$STUB_LOG" | head -1)
  echo "$build" | grep -q "b4af725398ecf4586cdb5d15c8637465de2d7614.tar.gz"
  echo "$build" | grep -q "c400969cff22d7be00cbd7c55487380ad876f0c2a814b206866ef9c1ae62dedb"
  [ "${build%%:*}" -lt "$(grep -n '^docker stop deluge$' "$STUB_LOG" | cut -d: -f1)" ]
}

@test "an already wired deluge is left untouched" {
  wire_once
  cp -p "$DELUGE_CONFIG/web.conf" "$STUB_DIR/web.conf.before"
  deluge_web
  run wire
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  ! grep -qE '^docker (stop|start|exec -u abc deluge sh)' "$STUB_LOG" || false
  cmp "$DELUGE_CONFIG/web.conf" "$STUB_DIR/web.conf.before"
}

@test "a drifted core setting is corrected and the rest of core.conf is kept" {
  printf 'deluge:\n  core:\n    max_upload_speed: 2000.0\n  plugins:\n    - name: Label\n' > "$CONFIG_DIR/apps.yml"
  wire_once
  python3 -c '
import sys
path = sys.argv[1]
text = open(path).read().replace("2000.0", "500.0")
open(path, "w").write(text)' "$DELUGE_CONFIG/core.conf"
  deluge_web
  run wire
  [ "$(conf_value core.conf max_upload_speed)" = 2000.0 ]
  [ "$(conf_value core.conf download_location)" = '"/downloads"' ]
  [ "$(grep -c '^docker stop deluge$' "$STUB_LOG")" -eq 1 ]
  echo "$output" | grep -q "deluge: set max_upload_speed 500.0 -> 2000.0"
}

@test "a changed web password is applied without knowing the old one" {
  wire_once
  printf 'DELUGE_WEB_PASSWORD=new pass\n' > "$ENGINE_DIR/.secrets/apps.env"
  deluge_web
  run wire
  [ "$status" -eq 0 ]
  password_matches "new pass"
}

@test "plugins enabled by hand are kept" {
  python3 -c '
import sys
path = sys.argv[1]
text = open(path).read().replace("\"enabled_plugins\": []", "\"enabled_plugins\": [\"Scheduler\"]")
open(path, "w").write(text)' "$DELUGE_CONFIG/core.conf"
  deluge_web
  run wire
  [ "$(conf_value core.conf enabled_plugins)" = '["Scheduler", "Label", "AutoRemovePlus"]' ]
}

@test "a dry run reports the changes and touches nothing" {
  cp "$DELUGE_CONFIG/core.conf" "$STUB_DIR/core.conf.before"
  deluge_web
  WIRE_DRY_RUN=1 run wire
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "(dry run) deluge: enable plugin Label"
  echo "$output" | grep -q "(dry run) deluge: build plugin AutoRemovePlus"
  cmp "$DELUGE_CONFIG/core.conf" "$STUB_DIR/core.conf.before"
  [ ! -e "$DELUGE_CONFIG/web.conf" ]
  ! grep -qE '^docker (stop|start|exec -u abc)' "$STUB_LOG" || false
}

@test "a failed plugin build fails the step but the settings are still applied" {
  deluge_web
  FAKE_BUILD_FAILS=1 run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "could not build plugin AutoRemovePlus"
  password_matches "web pass"
}

@test "a web ui that never accepts the password fails the step" {
  printf '{"effects": {"POST /json": {"respond": {"result": false, "error": null, "id": 1}}}}' > "$STUB_DIR/deluge-web.json"
  start_fake_app "$STUB_DIR/deluge-web.json"
  run wire
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "does not accept the web password"
}

@test "a web ui that drops connections while starting is waited for, not a crash" {
  python3 -c '
import socket, sys
server = socket.socket()
server.bind(("127.0.0.1", 0))
server.listen()
open(sys.argv[1], "w").write(str(server.getsockname()[1]))
while True:
    connection, _ = server.accept()
    connection.recv(65536)
    connection.close()' "$STUB_DIR/dropping-port" &
  dropping=$!
  while [ ! -s "$STUB_DIR/dropping-port" ]; do sleep 0.1; done
  DELUGE_URL="http://127.0.0.1:$(cat "$STUB_DIR/dropping-port")" run "$BATS_TEST_DIRNAME/../scripts/wire/30-deluge.sh"
  kill "$dropping"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "does not accept the web password"
  ! echo "$output" | grep -q Traceback || false
}
