load helpers

setup() {
  setup_stubs
  WIRE="$STUB_DIR/wire"
  mkdir -p "$WIRE"
  cp "$BATS_TEST_DIRNAME/../scripts/wire/wire-apps.sh" "$WIRE/"
  for step in 10-first 20-second 30-third; do
    printf '#!/usr/bin/env bash\necho %s >> "$STUB_LOG"\n[ "${FAIL_STEP:-}" != %s ]\n' "$step" "$step" > "$WIRE/$step.sh"
    chmod +x "$WIRE/$step.sh"
  done
}

teardown() {
  teardown_stubs
}

@test "wiring runs every step in order" {
  run "$WIRE/wire-apps.sh"
  [ "$status" -eq 0 ]
  [ "$(cat "$STUB_LOG" | tr '\n' ' ')" = "10-first 20-second 30-third " ]
}

@test "a failing step does not stop the others, but fails the run and is named" {
  FAIL_STEP=20-second run "$WIRE/wire-apps.sh"
  [ "$status" -ne 0 ]
  grep -q 30-third "$STUB_LOG"
  echo "$output" | grep -q "20-second"
}

@test "wiring with no steps succeeds" {
  rm "$WIRE"/[0-9]*.sh
  run "$WIRE/wire-apps.sh"
  [ "$status" -eq 0 ]
}

make_app_step() {
  printf '#!/usr/bin/env bash\necho %s >> "$STUB_LOG"\n' "$1" > "$WIRE/$1.sh"
  chmod +x "$WIRE/$1.sh"
}

@test "the engine wires the apps in dependency order" {
  [ "$(cd "$BATS_TEST_DIRNAME/../scripts/wire" && ls [0-9][0-9]-*.sh | tr '\n' ' ')" = \
    "10-prowlarr.sh 20-jellyfin.sh 25-library-updates.sh 30-deluge.sh 40-configarr.sh 50-seerr.sh 60-bazarr.sh 70-maintainerr.sh " ]
}

@test "a step waits until its app answers" {
  rm "$WIRE"/[0-9]*.sh
  make_app_step 10-prowlarr
  make_stub curl '[ "$(grep -c "^curl" "$STUB_LOG")" -ge 3 ]'
  WIRE_RETRY_SECONDS=0 run "$WIRE/wire-apps.sh"
  [ "$status" -eq 0 ]
  [ "$(grep -c '^curl .*http://localhost:9696/ping' "$STUB_LOG")" -eq 3 ]
  [ "$(tail -1 "$STUB_LOG")" = 10-prowlarr ]
}

@test "an app that never answers is skipped and named, and the rest still run" {
  rm "$WIRE"/[0-9]*.sh
  make_app_step 10-prowlarr
  make_app_step 20-jellyfin
  make_stub curl 'case "$*" in *9696*) exit 7 ;; esac'
  WIRE_WAIT_SECONDS=1 WIRE_RETRY_SECONDS=0 run "$WIRE/wire-apps.sh"
  [ "$status" -ne 0 ]
  ! grep -q '^10-prowlarr$' "$STUB_LOG" || false
  grep -q '^20-jellyfin$' "$STUB_LOG"
  echo "$output" | grep -q "prowlarr is not answering"
}

@test "steps without an app of their own do not wait" {
  rm "$WIRE"/[0-9]*.sh
  make_app_step 40-configarr
  make_stub curl 'exit 7'
  run "$WIRE/wire-apps.sh"
  [ "$status" -eq 0 ]
  ! grep -q '^curl' "$STUB_LOG" || false
}

@test "the jellyfin connection step waits for sonarr and radarr, which it changes" {
  rm "$WIRE"/[0-9]*.sh
  make_app_step 25-library-updates
  make_stub curl ''
  run "$WIRE/wire-apps.sh"
  [ "$status" -eq 0 ]
  grep -q '^curl .*http://localhost:8989/ping' "$STUB_LOG"
  grep -q '^curl .*http://localhost:7878/ping' "$STUB_LOG"
  [ "$(tail -1 "$STUB_LOG")" = 25-library-updates ]
}
