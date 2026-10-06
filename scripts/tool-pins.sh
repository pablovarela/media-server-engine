# shellcheck shell=bash

restic_url() {
  local platform=$1 version=$2
  echo "https://github.com/restic/restic/releases/download/v$version/restic_${version}_${platform}.bz2"
}

download_verified() {
  local url=$1 sha256=$2 out=$3
  curl -fsSL -o "$out" "$url"
  echo "$sha256  $out" | sha256sum -c -
}
