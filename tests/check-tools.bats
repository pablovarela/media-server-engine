load helpers

setup() {
  setup_stubs
  source "$BATS_TEST_DIRNAME/../scripts/tool-versions.env"
  for tool in git make curl sqlite3 python3; do make_stub "$tool" ''; done
  make_stub docker 'if [ "$1 $2" = "compose version" ]; then echo "Docker Compose version v5.4.0"; fi'
  make_stub sops "if [ \"\$1\" = --version ]; then echo \"sops ${SOPS_VERSION#v}\"; elif [ -n \"\${FAKE_BAD_KEY:-}\" ]; then exit 1; fi"
  make_stub age "echo ${AGE_VERSION}"
  make_stub restic "echo \"restic ${RESTIC_VERSION} compiled with go\""
  export PATH="$STUB_DIR:/usr/bin:/bin"
  export CHECK_PINNED_VERSIONS=1
}

teardown() {
  teardown_stubs
}

@test "check-tools passes when everything is installed and the key decrypts" {
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "OK .*age key decrypts secrets"
}

@test "check-tools fails and names a missing tool" {
  rm "$STUB_DIR/restic"
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "MISSING.*restic"
  echo "$output" | grep -q "make bootstrap"
}

@test "check-tools fails on a version that does not match the pin" {
  make_stub restic 'echo "restic 0.11.0 compiled with go"'
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "WRONG.*restic"
}

@test "check-tools ignores versions when pins are not checked" {
  make_stub restic 'echo "restic 0.11.0 compiled with go"'
  CHECK_PINNED_VERSIONS=0 run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -eq 0 ]
}

@test "check-tools fails when the age key cannot decrypt the secrets" {
  FAKE_BAD_KEY=1 run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "MISSING.*age key"
}

@test "check-tools reports every problem, not just the first" {
  rm "$STUB_DIR/restic" "$STUB_DIR/age"
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  echo "$output" | grep -q "MISSING.*age$"
  echo "$output" | grep -q "MISSING.*restic"
}

@test "check-tools rejects a version that only starts with the pinned one" {
  make_stub restic "echo \"restic ${RESTIC_VERSION}0 compiled with go\""
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "WRONG.*restic"
}

@test "check-tools checks python3, which the download cleanup needs" {
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  echo "$output" | grep -qE "^OK +python3$"
}
