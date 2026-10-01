# shellcheck shell=bash

reports_version() {
  local word
  for word in $1; do
    [ "${word#v}" = "${2#v}" ] && return 0
  done
  return 1
}

installed_version() {
  "$1" "$2" 2>&1 | head -1
}

pinned_version_installed() {
  command -v "$1" >/dev/null && reports_version "$(installed_version "$1" "$3")" "$2"
}

sops_url() {
  echo "https://github.com/getsops/sops/releases/download/$SOPS_VERSION/sops-$SOPS_VERSION.linux.arm64"
}

age_url() {
  echo "https://github.com/FiloSottile/age/releases/download/$AGE_VERSION/age-$AGE_VERSION-linux-arm64.tar.gz"
}

restic_url() {
  echo "https://github.com/restic/restic/releases/download/v$RESTIC_VERSION/restic_${RESTIC_VERSION}_linux_arm64.bz2"
}

download_verified() {
  local url=$1 sha256=$2 out=$3
  curl -fsSL -o "$out" "$url"
  echo "$sha256  $out" | sha256sum -c -
}
