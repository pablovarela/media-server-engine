load helpers

REPO="$BATS_TEST_DIRNAME/.."

sha256_of() {
  if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

release_json() {
  cat <<EOF
{
  "url": "https://api.github.com/repos/pablovarela/media-server-engine/releases/1",
  "tag_name": "$1",
  "name": "$1",
  "assets": [
    {
      "url": "https://api.github.com/repos/pablovarela/media-server-engine/releases/assets/101",
      "id": 101,
      "name": "mse_linux_arm64.tar.gz",
      "uploader": {
        "url": "https://api.github.com/users/github-actions%5Bbot%5D"
      }
    },
    {
      "url": "https://api.github.com/repos/pablovarela/media-server-engine/releases/assets/102",
      "id": 102,
      "name": "checksums.txt"
    }
  ]
}
EOF
}

setup() {
  setup_stubs
  FIXTURES="$STUB_DIR/fixtures"
  export FIXTURES
  mkdir -p "$FIXTURES/releases/tags" "$FIXTURES/releases/assets" "$STUB_DIR/package"
  printf '#!/bin/sh\necho "mse v0.7.0 (fake)"\n' > "$STUB_DIR/package/mse"
  chmod +x "$STUB_DIR/package/mse"
  tar -czf "$FIXTURES/releases/assets/101" -C "$STUB_DIR/package" mse
  printf '%s  mse_linux_arm64.tar.gz\n' "$(sha256_of "$FIXTURES/releases/assets/101")" > "$FIXTURES/releases/assets/102"
  release_json v0.7.0 > "$FIXTURES/releases/latest"
  release_json v0.6.9 > "$FIXTURES/releases/tags/v0.6.9"

  make_stub curl '
output="" url=""
while [ $# -gt 0 ]; do
  case $1 in
    -H) case $2 in @*) cat "${2#@}" >> "$STUB_LOG.headers" ;; esac; shift 2 ;;
    -o) output=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
file="$FIXTURES/${url#https://api.github.com/repos/pablovarela/media-server-engine/}"
[ -f "$file" ] || exit 22
case $url in *"${CURL_HANG_ON:-none}") touch "$STUB_LOG.hanging"; sleep 2 ;; esac
if [ -n "$output" ]; then cp "$file" "$output"; else cat "$file"; fi'
  make_stub uname '
case $1 in
  -s) echo "${FAKE_OS:-Linux}" ;;
  -m) echo "${FAKE_ARCH:-aarch64}" ;;
esac'
  make_stub gh 'exit 1'

  export GITHUB_TOKEN=test-token
  export MSE_INSTALL_DIR="$STUB_DIR/bin"
  unset MSE_VERSION
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
  grep -q "Authorization: Bearer test-token" "$STUB_LOG.headers"
  ! grep -q "test-token" "$STUB_LOG" || false
  grep -q "releases/latest" "$STUB_LOG"
  grep -q "Accept: application/octet-stream" "$STUB_LOG"
}

@test "works when piped into sh" {
  run sh -c "sh < '$REPO/install.sh'"

  [ "$status" -eq 0 ]
  [ "$("$MSE_INSTALL_DIR/mse")" = "mse v0.7.0 (fake)" ]
}

@test "installs the release MSE_VERSION names" {
  MSE_VERSION=v0.6.9 run sh "$REPO/install.sh"

  [ "$status" -eq 0 ]
  grep -q "releases/tags/v0.6.9" "$STUB_LOG"
  ! grep -q "releases/latest" "$STUB_LOG" || false
}

@test "uses the gh token when GITHUB_TOKEN is not set" {
  unset GITHUB_TOKEN
  make_stub gh '[ "$*" = "auth token" ] && echo gh-token'

  run sh "$REPO/install.sh"

  [ "$status" -eq 0 ]
  grep -q "Authorization: Bearer gh-token" "$STUB_LOG.headers"
}

@test "stops without a token" {
  unset GITHUB_TOKEN

  run sh "$REPO/install.sh"

  [ "$status" -eq 1 ]
  [[ "$output" == *"set GITHUB_TOKEN"* ]]
  [ ! -e "$MSE_INSTALL_DIR/mse" ]
}

@test "says the token may lack access when the latest release cannot be read" {
  rm "$FIXTURES/releases/latest"

  run sh "$REPO/install.sh"

  [ "$status" -eq 1 ]
  [[ "$output" == *"could not read the latest release of pablovarela/media-server-engine"* ]]
  [[ "$output" == *"token"* ]]
}

@test "names the release that does not exist" {
  MSE_VERSION=v9.9.9 run sh "$REPO/install.sh"

  [ "$status" -eq 1 ]
  [[ "$output" == *"no release v9.9.9 in pablovarela/media-server-engine, or the token cannot read it"* ]]
}

@test "picks the archive for the machine" {
  FAKE_OS=Darwin FAKE_ARCH=x86_64 run sh "$REPO/install.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"release v0.7.0 has no mse_darwin_amd64.tar.gz"* ]]

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
  printf '%s  mse_linux_arm64.tar.gz\n' "0000000000000000000000000000000000000000000000000000000000000000" > "$FIXTURES/releases/assets/102"

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
  export CURL_HANG_ON=releases/assets/101

  dash "$REPO/install.sh" > "$STUB_DIR/out" 2>&1 3>&- &
  pid=$!
  for _ in $(seq 50); do [ -e "$STUB_LOG.hanging" ] && break; sleep 0.1; done
  [ -e "$STUB_LOG.hanging" ]
  kill -TERM "$pid"
  wait "$pid" || true

  [ -z "$(ls -A "$TMPDIR")" ]
}
