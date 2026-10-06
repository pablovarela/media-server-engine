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

download() {
	curl -fsSL -o "$workdir/$1" "$base/$1"
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

if [ -n "${MSE_VERSION:-}" ]; then
	base=$RELEASES/download/$MSE_VERSION
	release="release $MSE_VERSION"
	download checksums.txt || fail "no release $MSE_VERSION in $REPOSITORY"
else
	base=$RELEASES/latest/download
	release="the latest release"
	download checksums.txt || fail "could not download the latest release of $REPOSITORY"
fi

expected=$(awk -v name="$archive" '$2 == name { print $1 }' "$workdir/checksums.txt")
[ -n "$expected" ] || fail "$release has no $archive"
download "$archive" || fail "could not download $archive from $release"
[ "$(sha256 "$workdir/$archive")" = "$expected" ] || fail "checksum mismatch for $archive in $release: not installing"

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
