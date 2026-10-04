load helpers

setup() {
  setup_stubs
  source "$BATS_TEST_DIRNAME/../scripts/tool-versions.env"
  for tool in git make curl; do make_stub "$tool" ''; done
  make_stub python3 'if [ "$*" = "-c import yaml" ] && [ -n "${FAKE_NO_YAML:-}" ]; then exit 1; fi; if [ "$*" = "-c import sqlite3" ] && [ -n "${FAKE_NO_SQLITE:-}" ]; then exit 1; fi'
  make_stub docker '
if [ "$1 $2" = "compose version" ]; then echo "Docker Compose version v5.4.0"; fi
if [ "$1" = info ] && [ -n "${FAKE_DOCKER_DENIED:-}" ]; then echo "permission denied while trying to connect to the docker API at unix:///var/run/docker.sock" >&2; exit 1; fi
if [ "$1" = info ] && [ -n "${FAKE_DOCKER_DOWN:-}" ]; then echo "Cannot connect to the Docker daemon. Is the docker daemon running?" >&2; exit 1; fi'
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

@test "check-tools decrypts the secrets from the config directory" {
  make_stub sops "if [ \"\$1\" = --version ]; then echo \"sops ${SOPS_VERSION#v}\"; else echo \"cwd=\$PWD \$*\" >> \"\$STUB_LOG\"; fi"
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  grep -q "cwd=.*$(basename "$CONFIG_DIR") decrypt secrets/vpn.sops.env" "$STUB_LOG"
}

@test "check-tools checks the python yaml module, which the app wiring needs" {
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  echo "$output" | grep -qE "^OK +python3 yaml module$"
  FAKE_NO_YAML=1 run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "MISSING.*python3 yaml module"
}

@test "whiptail is optional: its absence is reported but is not a problem" {
  WHIPTAIL_COMMAND=whiptail-that-is-not-installed run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -eq 0 ]
  echo "$output" | grep -qE "^OPTIONAL +whiptail"
  echo "$output" | grep -q "plain questions"
  make_stub whiptail ''
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  echo "$output" | grep -qE "^OK +whiptail"
}

@test "a user just added to the docker group is told to log in again" {
  FAKE_DOCKER_DENIED=1 run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "log out and back in"
  echo "$output" | grep -q "make setup-machine"
}

@test "a docker that is not running is reported" {
  FAKE_DOCKER_DOWN=1 run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "docker is not running"
}

@test "check-tools checks perl and openssl, which configure and the backups use" {
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  echo "$output" | grep -q "OK .*perl$"
  echo "$output" | grep -q "OK .*openssl$"
}

@test "the sqlite3 command is not needed" {
  ! grep -E "^for tool in " "$BATS_TEST_DIRNAME/../scripts/check-tools.sh" | grep -qw sqlite3 || false
  ! grep -E "apt-get install" "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh" | grep -qw sqlite3 || false
}

@test "check-tools checks the python sqlite3 module, which the backup check needs" {
  run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  echo "$output" | grep -qE "^OK +python3 sqlite3 module$"
  FAKE_NO_SQLITE=1 run "$BATS_TEST_DIRNAME/../scripts/check-tools.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "MISSING.*python3 sqlite3 module"
}
