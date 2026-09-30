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
  ! grep -q "apt-get" "$STUB_LOG"
  ! grep -q "curl" "$STUB_LOG"
}

@test "bootstrap refuses an operating system it does not support" {
  FAKE_OS=FreeBSD run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "FreeBSD"
  ! grep -qE "brew|apt-get|curl" "$STUB_LOG"
}

@test "bootstrap refuses Linux on a CPU the pinned binaries do not support" {
  FAKE_OS=Linux FAKE_ARCH=x86_64 run "$BATS_TEST_DIRNAME/../scripts/bootstrap.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "x86_64"
  ! grep -qE "apt-get|curl" "$STUB_LOG"
}
