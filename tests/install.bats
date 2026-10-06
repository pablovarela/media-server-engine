load helpers

REPO="$BATS_TEST_DIRNAME/.."

sha256_of() {
  if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

setup() {
  setup_stubs
  FIXTURES="$STUB_DIR/fixtures"
  export FIXTURES
  mkdir -p "$FIXTURES/latest/download" "$FIXTURES/download/v0.6.9" "$STUB_DIR/package"
  printf '#!/bin/sh\necho "mse v0.7.0 (fake)"\n' > "$STUB_DIR/package/mse"
  chmod +x "$STUB_DIR/package/mse"
  tar -czf "$FIXTURES/latest/download/mse_linux_arm64.tar.gz" -C "$STUB_DIR/package" mse
  printf '%s  mse_linux_arm64.tar.gz\n' "$(sha256_of "$FIXTURES/latest/download/mse_linux_arm64.tar.gz")" > "$FIXTURES/latest/download/checksums.txt"
  cp "$FIXTURES/latest/download/"* "$FIXTURES/download/v0.6.9/"

  make_stub curl '
output="" url=""
while [ $# -gt 0 ]; do
  case $1 in
    -H) echo "header $2" >> "$STUB_LOG.headers"; shift 2 ;;
    -o) output=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
file="$FIXTURES/${url#https://github.com/pablovarela/media-server-engine/releases/}"
[ -f "$file" ] || exit 22
case $url in *"${CURL_HANG_ON:-none}") touch "$STUB_LOG.hanging"; sleep 2 ;; esac
if [ -n "$output" ]; then cp "$file" "$output"; else cat "$file"; fi'
  make_stub uname '
case $1 in
  -s) echo "${FAKE_OS:-Linux}" ;;
  -m) echo "${FAKE_ARCH:-aarch64}" ;;
esac'
  make_stub gh 'exit 1'

  unset GITHUB_TOKEN GH_TOKEN MSE_VERSION
  export MSE_INSTALL_DIR="$STUB_DIR/bin"
}

teardown() {
  teardown_stubs
}

@test "installs the latest release, replacing the previous mse" {
  mkdir -p "$MSE_INSTALL_DIR"
  printf '#!/bin/sh\necho old\n' > "$MSE_INSTALL_DIR/mse"
  chmod +x "$MSE_INSTALL_DIR/mse"

  run sh "$REPO/install.sh"

  [ "$status" -eq 0 ]
  [[ "$output" == *"mse v0.7.0 (fake)"* ]]
  [ -x "$MSE_INSTALL_DIR/mse" ]
  [ "$("$MSE_INSTALL_DIR/mse")" = "mse v0.7.0 (fake)" ]
  grep -q "github.com/pablovarela/media-server-engine/releases/latest/download/mse_linux_arm64.tar.gz" "$STUB_LOG"
  grep -q "github.com/pablovarela/media-server-engine/releases/latest/download/checksums.txt" "$STUB_LOG"
}

@test "works when piped into sh" {
  run sh -c "sh < '$REPO/install.sh'"

  [ "$status" -eq 0 ]
  [ "$("$MSE_INSTALL_DIR/mse")" = "mse v0.7.0 (fake)" ]
}

@test "installs the release MSE_VERSION names" {
  MSE_VERSION=v0.6.9 run sh "$REPO/install.sh"

  [ "$status" -eq 0 ]
  grep -q "releases/download/v0.6.9/mse_linux_arm64.tar.gz" "$STUB_LOG"
  ! grep -q "releases/latest" "$STUB_LOG" || false
}

@test "needs no GitHub login or token, and sends none" {
  GITHUB_TOKEN=a-token run sh "$REPO/install.sh"

  [ "$status" -eq 0 ]
  [ "$("$MSE_INSTALL_DIR/mse")" = "mse v0.7.0 (fake)" ]
  [ ! -e "$STUB_LOG.headers" ]
  ! grep -q "gh " "$STUB_LOG" || false
}

@test "says so when the latest release cannot be downloaded" {
  rm "$FIXTURES/latest/download/checksums.txt"

  run sh "$REPO/install.sh"

  [ "$status" -eq 1 ]
  [[ "$output" == *"could not download the latest release of pablovarela/media-server-engine"* ]]
  [ ! -e "$MSE_INSTALL_DIR/mse" ]
}

@test "names the release that does not exist" {
  MSE_VERSION=v9.9.9 run sh "$REPO/install.sh"

  [ "$status" -eq 1 ]
  [[ "$output" == *"no release v9.9.9 in pablovarela/media-server-engine"* ]]
}

@test "picks the archive for the machine" {
  FAKE_OS=Darwin FAKE_ARCH=x86_64 run sh "$REPO/install.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"the latest release has no mse_darwin_amd64.tar.gz"* ]]

  FAKE_OS=Linux FAKE_ARCH=amd64 run sh "$REPO/install.sh"
  [[ "$output" == *"has no mse_linux_amd64.tar.gz"* ]]

  FAKE_OS=Darwin FAKE_ARCH=arm64 run sh "$REPO/install.sh"
  [[ "$output" == *"has no mse_darwin_arm64.tar.gz"* ]]
}

@test "stops on an unsupported machine" {
  FAKE_ARCH=armv7l run sh "$REPO/install.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"unsupported architecture: armv7l"* ]]

  FAKE_OS=FreeBSD run sh "$REPO/install.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"unsupported operating system: FreeBSD"* ]]
}

@test "keeps the previous mse when the checksum does not match" {
  mkdir -p "$MSE_INSTALL_DIR"
  printf '#!/bin/sh\necho old\n' > "$MSE_INSTALL_DIR/mse"
  chmod +x "$MSE_INSTALL_DIR/mse"
  printf '%s  mse_linux_arm64.tar.gz\n' "0000000000000000000000000000000000000000000000000000000000000000" > "$FIXTURES/latest/download/checksums.txt"

  run sh "$REPO/install.sh"

  [ "$status" -eq 1 ]
  [[ "$output" == *"checksum mismatch for mse_linux_arm64.tar.gz"* ]]
  [ "$("$MSE_INSTALL_DIR/mse")" = "old" ]
  [ -z "$(find "$MSE_INSTALL_DIR" -name '.mse.*')" ]
}

@test "warns when the install directory is not on PATH" {
  run sh "$REPO/install.sh"
  [[ "$output" == *"$MSE_INSTALL_DIR is not on PATH"* ]]

  PATH="$MSE_INSTALL_DIR:$PATH" run sh "$REPO/install.sh"
  [[ "$output" != *"is not on PATH"* ]]
}

@test "removes its temporary files when stopped" {
  export TMPDIR="$STUB_DIR/tmp"
  mkdir -p "$TMPDIR"
  export CURL_HANG_ON=mse_linux_arm64.tar.gz

  dash "$REPO/install.sh" > "$STUB_DIR/out" 2>&1 3>&- &
  pid=$!
  for _ in $(seq 50); do [ -e "$STUB_LOG.hanging" ] && break; sleep 0.1; done
  [ -e "$STUB_LOG.hanging" ]
  kill -TERM "$pid"
  wait "$pid" || true

  [ -z "$(ls -A "$TMPDIR")" ]
}
