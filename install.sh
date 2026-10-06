#!/bin/sh
set -eu

REPOSITORY=pablovarela/media-server-engine
RELEASES=https://github.com/$REPOSITORY/releases

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

platform() {
	case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
	esac
	echo "${os}_${arch}"
}

latest_tag() {
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES/latest") || return 1
	case $latest in
	"$RELEASES/tag/"?*) echo "${latest#"$RELEASES/tag/"}" ;;
	*) return 1 ;;
	esac
}

download() {
	code=$(curl -sSL -o "$workdir/$1" -w '%{http_code}' "$RELEASES/download/$tag/$1") ||
		fail "could not reach github.com to download $1 of release $tag"
	case $code in
	200) ;;
	404) return 1 ;;
	*) fail "GitHub answered $code for $1 of release $tag" ;;
	esac
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi | cut -d' ' -f1
}

archive="mse_$(platform).tar.gz"

workdir=$(mktemp -d "${TMPDIR:-/tmp}/mse-install.XXXXXX")
staged=""
trap 'rm -rf "$workdir"; [ -z "$staged" ] || rm -f "$staged"' EXIT
trap 'exit 1' HUP INT TERM

tag=${MSE_VERSION:-}
if [ -z "$tag" ]; then
	tag=$(latest_tag) || fail "could not find the latest release of $REPOSITORY"
fi

download checksums.txt || fail "no release $tag in $REPOSITORY"
expected=$(awk -v name="$archive" '$2 == name { print $1 }' "$workdir/checksums.txt")
[ -n "$expected" ] || fail "release $tag has no $archive"
download "$archive" || fail "release $tag has no $archive"
[ "$(sha256 "$workdir/$archive")" = "$expected" ] || fail "checksum mismatch for $archive in release $tag: not installing"

install_dir=${MSE_INSTALL_DIR:-$HOME/.local/bin}

tar -xzf "$workdir/$archive" -C "$workdir" mse
mkdir -p "$install_dir"
staged=$(mktemp "$install_dir/.mse.XXXXXX")
cp "$workdir/mse" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$install_dir/mse"
staged=""

case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "install.sh: $install_dir is not on PATH; add it to run mse by name" >&2 ;;
esac
"$install_dir/mse" version
