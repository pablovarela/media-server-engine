load helpers

setup() {
  setup_stubs
  source "$BATS_TEST_DIRNAME/../scripts/tool-versions.env"
  source "$BATS_TEST_DIRNAME/../scripts/tool-pins.sh"
}

teardown() {
  teardown_stubs
}

@test "each pinned tool is downloaded from its release, for the pinned version" {
  [ "$(sops_url)" = "https://github.com/getsops/sops/releases/download/$SOPS_VERSION/sops-$SOPS_VERSION.linux.arm64" ]
  [ "$(age_url)" = "https://github.com/FiloSottile/age/releases/download/$AGE_VERSION/age-$AGE_VERSION-linux-arm64.tar.gz" ]
  [ "$(restic_url)" = "https://github.com/restic/restic/releases/download/v$RESTIC_VERSION/restic_${RESTIC_VERSION}_linux_arm64.bz2" ]
}

@test "the pins name a version once, so a version bump is a one-line change" {
  for tool in SOPS AGE RESTIC; do
    [ "$(grep -c "^${tool}_" "$BATS_TEST_DIRNAME/../scripts/tool-versions.env")" -eq 2 ]
  done
}

@test "the download check passes when every pinned file matches its checksum" {
  make_stub curl 'touch "${@: -1}"'
  make_stub sha256sum 'cat >> "$STUB_LOG"'
  run "$BATS_TEST_DIRNAME/../scripts/check-tool-downloads.sh"
  [ "$status" -eq 0 ]
  grep -q "^$SOPS_SHA256 " "$STUB_LOG"
  grep -q "^$AGE_SHA256 " "$STUB_LOG"
  grep -q "^$RESTIC_SHA256 " "$STUB_LOG"
}

@test "the download check fails and names the tool whose checksum does not match" {
  make_stub curl 'touch "${@: -1}"'
  make_stub sha256sum 'if grep -q "$RESTIC_SHA256"; then exit 1; fi'
  RESTIC_SHA256=$RESTIC_SHA256 run "$BATS_TEST_DIRNAME/../scripts/check-tool-downloads.sh"
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "restic $RESTIC_VERSION"
  ! echo "$output" | grep -q "sops $SOPS_VERSION.*does not match" || false
}
