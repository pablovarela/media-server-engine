#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
# shellcheck source=scripts/tool-versions.env
source scripts/tool-versions.env
# shellcheck source=scripts/tool-pins.sh
source scripts/tool-pins.sh

downloads=$(mktemp -d)
trap 'rm -rf "$downloads"' EXIT
mismatches=0

check() {
  local name=$1 version=$2 url=$3 sha256=$4
  if download_verified "$url" "$sha256" "$downloads/$name" >/dev/null 2>&1; then
    echo "OK       $name $version"
  else
    echo "MISMATCH $name $version: $url does not match the SHA256 pinned in scripts/tool-versions.env"
    mismatches=$((mismatches + 1))
  fi
}

check sops "$SOPS_VERSION" "$(sops_url)" "$SOPS_SHA256"
check age "$AGE_VERSION" "$(age_url)" "$AGE_SHA256"
check restic "$RESTIC_VERSION" "$(restic_url)" "$RESTIC_SHA256"

mse_restic_version=$(grep '^RESTIC_VERSION=' internal/restic/release.env | cut -d= -f2)
for platform in linux_arm64 linux_amd64 darwin_arm64 darwin_amd64; do
  sha256=$(grep "^RESTIC_SHA256_${platform}=" internal/restic/release.env | cut -d= -f2)
  url="https://github.com/restic/restic/releases/download/v${mse_restic_version}/restic_${mse_restic_version}_${platform}.bz2"
  if download_verified "$url" "$sha256" "$downloads/restic-$platform" >/dev/null 2>&1; then
    echo "OK       restic $platform $mse_restic_version"
  else
    echo "MISMATCH restic $platform $mse_restic_version: $url does not match the SHA256 pinned in internal/restic/release.env"
    mismatches=$((mismatches + 1))
  fi
done
[ "$mismatches" -eq 0 ]
