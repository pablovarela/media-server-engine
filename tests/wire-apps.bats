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
