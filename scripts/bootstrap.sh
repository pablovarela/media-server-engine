#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
# shellcheck source=scripts/tool-versions.env
source scripts/tool-versions.env
# shellcheck source=scripts/tool-pins.sh
source scripts/tool-pins.sh

install_apt_packages() {
  sudo apt-get update
  sudo apt-get install -y curl git sqlite3 python3 python3-yaml make bzip2 perl openssl
}

readonly NO_MENUS="whiptail could not be installed; make configure asks plain questions instead of menus."

install_whiptail_debian() {
  sudo apt-get install -y whiptail || echo "$NO_MENUS" >&2
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
  download_verified "$(sops_url)" "$SOPS_SHA256" "$tmp/sops"
  sudo install -m 755 "$tmp/sops" /usr/local/bin/sops
  rm -rf "$tmp"
}

install_age() {
  local tmp
  tmp=$(mktemp -d)
  download_verified "$(age_url)" "$AGE_SHA256" "$tmp/age.tgz"
  tar xzf "$tmp/age.tgz" -C "$tmp"
  sudo install -m 755 "$tmp/age/age" "$tmp/age/age-keygen" /usr/local/bin/
  rm -rf "$tmp"
}

install_restic() {
  local tmp
  tmp=$(mktemp -d)
  download_verified "$(restic_url)" "$RESTIC_SHA256" "$tmp/restic.bz2"
  bunzip2 "$tmp/restic.bz2"
  sudo install -m 755 "$tmp/restic" /usr/local/bin/restic
  rm -rf "$tmp"
}

bootstrap_debian() {
  install_apt_packages
  install_whiptail_debian
  install_docker
  install_sops
  install_age
  install_restic
}

install_pinned_tools_that_differ() {
  pinned_version_installed sops "$SOPS_VERSION" --version || { echo "Installing sops $SOPS_VERSION"; install_sops; }
  pinned_version_installed age "$AGE_VERSION" --version || { echo "Installing age $AGE_VERSION"; install_age; }
  pinned_version_installed restic "$RESTIC_VERSION" version || { echo "Installing restic $RESTIC_VERSION"; install_restic; }
}

bootstrap_macos() {
  brew install sops age restic
  brew install newt || echo "$NO_MENUS" >&2
  python3 -c 'import yaml' 2>/dev/null || python3 -m pip install --user --break-system-packages pyyaml
  command -v docker >/dev/null || echo "Docker is not installed; install OrbStack or Docker Desktop." >&2
}

require_arm64() {
  case $(uname -m) in
    aarch64 | arm64) ;;
    *) echo "bootstrap: the pinned Linux binaries are arm64; this machine is $(uname -m)" >&2; exit 1 ;;
  esac
}

if [ "${1:-}" = --pinned-tools ]; then
  if [ "$(uname -s)" = Linux ]; then require_arm64 && install_pinned_tools_that_differ; fi
  exit 0
fi

case $(uname -s) in
  Linux) require_arm64 && bootstrap_debian ;;
  Darwin) bootstrap_macos ;;
  *) echo "bootstrap: unsupported operating system $(uname -s)" >&2; exit 1 ;;
esac
sops --version
age --version
restic version
