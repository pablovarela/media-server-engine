#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/tool-versions.env
source scripts/tool-versions.env

download_verified() {
  local url=$1 sha256=$2 out=$3
  curl -fsSL -o "$out" "$url"
  echo "$sha256  $out" | sha256sum -c -
}

install_apt_packages() {
  sudo apt-get update
  sudo apt-get install -y curl git sqlite3 python3 python3-yaml make bzip2
}

install_docker() {
  if ! command -v docker >/dev/null; then
    curl -fsSL https://get.docker.com | sudo sh
    sudo usermod -aG docker "$USER"
  fi
}

install_sops() {
  local tmp
  tmp=$(mktemp -d)
  download_verified "https://github.com/getsops/sops/releases/download/$SOPS_VERSION/$SOPS_ASSET" "$SOPS_SHA256" "$tmp/sops"
  sudo install -m 755 "$tmp/sops" /usr/local/bin/sops
  rm -rf "$tmp"
}

install_age() {
  local tmp
  tmp=$(mktemp -d)
  download_verified "https://github.com/FiloSottile/age/releases/download/$AGE_VERSION/$AGE_ASSET" "$AGE_SHA256" "$tmp/age.tgz"
  tar xzf "$tmp/age.tgz" -C "$tmp"
  sudo install -m 755 "$tmp/age/age" "$tmp/age/age-keygen" /usr/local/bin/
  rm -rf "$tmp"
}

install_restic() {
  local tmp
  tmp=$(mktemp -d)
  download_verified "https://github.com/restic/restic/releases/download/v$RESTIC_VERSION/$RESTIC_ASSET" "$RESTIC_SHA256" "$tmp/restic.bz2"
  bunzip2 "$tmp/restic.bz2"
  sudo install -m 755 "$tmp/restic" /usr/local/bin/restic
  rm -rf "$tmp"
}

bootstrap_debian() {
  install_apt_packages
  install_docker
  install_sops
  install_age
  install_restic
}

bootstrap_macos() {
  brew install sops age restic
  python3 -c 'import yaml' 2>/dev/null || python3 -m pip install --user --break-system-packages pyyaml
  command -v docker >/dev/null || echo "Docker is not installed; install OrbStack or Docker Desktop." >&2
}

require_arm64() {
  case $(uname -m) in
    aarch64 | arm64) ;;
    *) echo "bootstrap: the pinned Linux binaries are arm64; this machine is $(uname -m)" >&2; exit 1 ;;
  esac
}

case $(uname -s) in
  Linux) require_arm64 && bootstrap_debian ;;
  Darwin) bootstrap_macos ;;
  *) echo "bootstrap: unsupported operating system $(uname -s)" >&2; exit 1 ;;
esac
sops --version
age --version
restic version
