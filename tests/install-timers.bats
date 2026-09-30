load helpers

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/systemd"
  cp "$BATS_TEST_DIRNAME"/../systemd/* "$ENGINE_DIR/systemd/"
  export UNIT_DIR="$STUB_DIR/units"
  mkdir -p "$UNIT_DIR"
  make_stub systemctl ''
  make_stub sudo '"$@"'
}

teardown() {
  teardown_stubs
}

@test "install renders the units for this checkout and user" {
  run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh" media-backup media-verify
  [ "$status" -eq 0 ]
  grep -q "^WorkingDirectory=$ENGINE_DIR$" "$UNIT_DIR/media-backup.service"
  grep -q "^Environment=CONFIG_DIR=$(cd "$CONFIG_DIR" && pwd)$" "$UNIT_DIR/media-backup.service"
  grep -q "^Environment=DATA_DIR=$(cd "$DATA_DIR" && pwd)$" "$UNIT_DIR/media-backup.service"
  grep -q "exec-env $(cd "$CONFIG_DIR" && pwd)/secrets/backup.sops.env scripts/backup.sh$" "$UNIT_DIR/media-backup.service"
  grep -q "^User=$(id -un)$" "$UNIT_DIR/media-backup.service"
  grep -q "^Environment=SOPS_AGE_KEY_FILE=$HOME/.config/sops/age/keys.txt$" "$UNIT_DIR/media-verify.service"
  ! grep -q "@" "$UNIT_DIR"/media-*
}

@test "install enables both timers" {
  run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh" media-backup media-verify
  grep -q "systemctl daemon-reload" "$STUB_LOG"
  grep -q "systemctl enable --now media-backup.timer media-verify.timer" "$STUB_LOG"
}

@test "install refuses on a machine without systemd" {
  rm "$STUB_DIR/systemctl"
  PATH="$STUB_DIR:/usr/bin:/bin" run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh" media-backup media-verify
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "systemd"
  [ -z "$(ls "$UNIT_DIR")" ]
}

@test "install renders and enables only the timers it is given" {
  run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh" media-download-cleanup
  [ "$status" -eq 0 ]
  [ "$(ls "$UNIT_DIR" | tr '\n' ' ')" = "media-download-cleanup.service media-download-cleanup.timer " ]
  grep -q "^ExecStart=$ENGINE_DIR/scripts/remove-executable-downloads.sh$" "$UNIT_DIR/media-download-cleanup.service"
  grep -q "systemctl enable --now media-download-cleanup.timer$" "$STUB_LOG"
}

@test "install refuses without unit names" {
  run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh"
  [ "$status" -ne 0 ]
  ! grep -q "systemctl" "$STUB_LOG"
}

@test "every unit in systemd/ renders with no placeholder left" {
  names=$(ls "$BATS_TEST_DIRNAME"/../systemd/*.timer | xargs -n1 basename | sed 's/\.timer$//' | tr '\n' ' ')
  run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh" $names
  [ "$status" -eq 0 ]
  [ "$(ls "$UNIT_DIR" | wc -l | tr -d ' ')" -eq "$(ls "$BATS_TEST_DIRNAME"/../systemd | wc -l | tr -d ' ')" ]
  ! grep -l "@[A-Z_]*@" "$UNIT_DIR"/*
}

@test "install writes normalized paths when config and data use the default relative location" {
  mkdir -p "$ENGINE_DIR/../config-rel" "$ENGINE_DIR/../data-rel"
  CONFIG_DIR="$ENGINE_DIR/../config-rel" DATA_DIR="$ENGINE_DIR/../data-rel" run "$BATS_TEST_DIRNAME/../scripts/install-timers.sh" media-backup
  [ "$status" -eq 0 ]
  ! grep -q "\.\./" "$UNIT_DIR/media-backup.service"
  rm -rf "$ENGINE_DIR/../config-rel" "$ENGINE_DIR/../data-rel"
}
