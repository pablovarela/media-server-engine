load helpers

setup() {
  setup_stubs
  make_stub uname 'if [ "$1" = -m ]; then echo "${FAKE_ARCH:-arm64}"; else echo "${FAKE_OS:-Darwin}"; fi'
  for tool in brew sudo apt-get curl; do make_stub "$tool" ''; done
  for tool in sops age restic; do make_stub "$tool" 'echo version'; done
}

teardown() {
  teardown_stubs
}

@test "bootstrap on macOS installs the tools with Homebrew" {
  run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  grep -q "brew install sops age restic" "$STUB_LOG"
  ! grep -q "apt-get" "$STUB_LOG" || false
  ! grep -q "curl" "$STUB_LOG" || false
}

@test "bootstrap refuses an operating system it does not support" {
  FAKE_OS=FreeBSD run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "FreeBSD"
  ! grep -qE "brew|apt-get|curl" "$STUB_LOG" || false
}

@test "bootstrap refuses Linux on a CPU the pinned binaries do not support" {
  FAKE_OS=Linux FAKE_ARCH=x86_64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "x86_64"
  ! grep -qE "apt-get|curl" "$STUB_LOG" || false
}

@test "bootstrap on Linux installs the python yaml module with apt" {
  for tool in sha256sum tar docker; do make_stub "$tool" ''; done
  FAKE_OS=Linux FAKE_ARCH=aarch64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  grep -q "sudo apt-get install -y .*python3-yaml" "$STUB_LOG"
}

@test "bootstrap on macOS adds the python yaml module only when it is missing" {
  make_stub python3 'if [ "$*" = "-c import yaml" ]; then [ -z "${FAKE_NO_YAML:-}" ]; fi'
  run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  ! grep -q "pip install" "$STUB_LOG" || false
  FAKE_NO_YAML=1 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  grep -q "python3 -m pip install --user --break-system-packages pyyaml" "$STUB_LOG"
}

@test "bootstrap on macOS tries to add whiptail for the configure menus" {
  run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  grep -q "^brew install newt" "$STUB_LOG"
}

@test "bootstrap carries on when whiptail cannot be installed" {
  make_stub brew '[ "$2" != newt ]'
  run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "plain questions"
}

@test "bootstrap on Linux adds whiptail with apt, separately from the required packages" {
  for tool in sha256sum tar docker; do make_stub "$tool" ''; done
  FAKE_OS=Linux FAKE_ARCH=aarch64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  grep -q "sudo apt-get install -y whiptail" "$STUB_LOG"
}

@test "bootstrap on Linux installs neither perl nor openssl" {
  for tool in sha256sum tar docker; do make_stub "$tool" ''; done
  FAKE_OS=Linux FAKE_ARCH=aarch64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  grep -q "sudo apt-get install -y curl git" "$STUB_LOG"
  ! grep "sudo apt-get install -y curl git" "$STUB_LOG" | grep -qwE "perl|openssl" || false
}

pinned_tool_stubs() {
  source "$BATS_TEST_DIRNAME/../scripts/tool-versions.env"
  for tool in sha256sum tar bunzip2; do make_stub "$tool" ''; done
  make_stub sops "echo \"sops ${SOPS_VERSION#v} (latest)\""
  make_stub age "echo ${AGE_VERSION}"
  make_stub restic "echo \"restic \${FAKE_RESTIC_VERSION:-$RESTIC_VERSION} compiled with go\""
}

@test "pinned tools are left alone when the installed versions match the pins" {
  pinned_tool_stubs
  FAKE_OS=Linux FAKE_ARCH=aarch64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh" --pinned-tools
  [ "$status" -eq 0 ]
  ! grep -qE "^(curl|sudo|apt-get)" "$STUB_LOG" || false
}

@test "only a pinned tool whose installed version differs is installed again" {
  pinned_tool_stubs
  FAKE_RESTIC_VERSION=0.17.0 FAKE_OS=Linux FAKE_ARCH=aarch64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh" --pinned-tools
  [ "$status" -eq 0 ]
  grep -q "^curl .*restic/releases" "$STUB_LOG"
  ! grep -q "^curl .*sops/releases" "$STUB_LOG" || false
  ! grep -q "^curl .*age/releases" "$STUB_LOG" || false
  ! grep -q "^apt-get" "$STUB_LOG" || false
  echo "$output" | grep -q "restic"
}

@test "pinned tools are not managed on macOS, where Homebrew installs them" {
  pinned_tool_stubs
  run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh" --pinned-tools
  [ "$status" -eq 0 ]
  ! grep -qE "^(brew|curl|sudo)" "$STUB_LOG" || false
}
