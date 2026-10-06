load helpers

PINS="$BATS_TEST_DIRNAME/../internal/restic/release.env"

setup() {
  setup_stubs
  source "$BATS_TEST_DIRNAME/../scripts/tool-pins.sh"
  RESTIC_VERSION=$(grep '^RESTIC_VERSION=' "$PINS" | cut -d= -f2)
}

teardown() {
  teardown_stubs
}

@test "restic is downloaded from its release, for the pinned version and platform" {
  [ "$(restic_url linux_arm64 "$RESTIC_VERSION")" = "https://github.com/restic/restic/releases/download/v$RESTIC_VERSION/restic_${RESTIC_VERSION}_linux_arm64.bz2" ]
}

@test "the download check verifies restic for every platform mse pins" {
  make_stub curl 'echo "$@" >> "$STUB_LOG.urls"; touch "${@: -1}"'
  make_stub sha256sum 'cat >> "$STUB_LOG"'
  run "$BATS_TEST_DIRNAME/../scripts/check-tool-downloads.sh"
  [ "$status" -eq 0 ]
  for platform in linux_arm64 linux_amd64 darwin_arm64 darwin_amd64; do
    sum=$(grep "^RESTIC_SHA256_${platform}=" "$PINS" | cut -d= -f2)
    grep -q "^$sum " "$STUB_LOG"
    grep -q "restic_${RESTIC_VERSION}_${platform}.bz2" "$STUB_LOG.urls"
  done
}

@test "the download check names the platform whose restic pin is wrong" {
  bad=$(grep '^RESTIC_SHA256_darwin_amd64=' "$PINS" | cut -d= -f2)
  make_stub curl 'touch "${@: -1}"'
  make_stub sha256sum "if grep -q $bad; then exit 1; fi"
  run "$BATS_TEST_DIRNAME/../scripts/check-tool-downloads.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "MISMATCH restic darwin_amd64"
  ! echo "$output" | grep -q "MISMATCH restic linux_arm64" || false
}
