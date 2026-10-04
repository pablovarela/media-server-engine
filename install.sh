#!/bin/sh
set -eu

REPOSITORY=pablovarela/media-server-engine
API=https://api.github.com/repos/$REPOSITORY

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

github_token() {
	if [ -n "${GITHUB_TOKEN:-}" ]; then
		echo "$GITHUB_TOKEN"
	elif command -v gh >/dev/null 2>&1 && gh auth token 2>/dev/null; then
		:
	else
		fail "set GITHUB_TOKEN to a token that can read $REPOSITORY, or log in with gh auth login"
	fi
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

api() {
	curl -fsSL -H "@$workdir/authorization" -H "Accept: application/vnd.github+json" "$API/$1"
}

download_asset() {
	curl -fsSL -H "@$workdir/authorization" -H "Accept: application/octet-stream" -o "$2" "$API/releases/assets/$1"
}

asset_id() {
	tr ',{}' '[\n*]' |
		sed -n \
			-e 's#^ *"url": *"[^"]*/releases/assets/\([0-9][0-9]*\)" *$#id \1#p' \
			-e 's#^ *"name": *"\([^"]*\)" *$#name \1#p' |
		awk -v want="$1" '$1 == "id" { id = $2 } $1 == "name" && $2 == want { print id; exit }'
}

tag_of() {
	tr ',' '\n' | sed -n 's#^ *"tag_name": *"\([^"]*\)".*#\1#p'
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1"
	else
		shasum -a 256 "$1"
	fi | cut -d' ' -f1
}

token=$(github_token)
archive="mse_$(platform).tar.gz"

workdir=$(mktemp -d "${TMPDIR:-/tmp}/mse-install.XXXXXX")
staged=""
trap 'rm -rf "$workdir"; [ -z "$staged" ] || rm -f "$staged"' EXIT
trap 'exit 1' HUP INT TERM
printf 'Authorization: Bearer %s\n' "$token" > "$workdir/authorization"

if [ -n "${MSE_VERSION:-}" ]; then
	release=$(api "releases/tags/$MSE_VERSION") || fail "no release $MSE_VERSION in $REPOSITORY, or the token cannot read it"
else
	release=$(api releases/latest) || fail "could not read the latest release of $REPOSITORY: check the token can read the repository"
fi
tag=$(printf '%s\n' "$release" | tag_of)

archive_id=$(printf '%s\n' "$release" | asset_id "$archive")
[ -n "$archive_id" ] || fail "release $tag has no $archive"
checksums_id=$(printf '%s\n' "$release" | asset_id checksums.txt)
[ -n "$checksums_id" ] || fail "release $tag has no checksums.txt"

install_dir=${MSE_INSTALL_DIR:-$HOME/.local/bin}

download_asset "$archive_id" "$workdir/$archive" || fail "could not download $archive from release $tag"
download_asset "$checksums_id" "$workdir/checksums.txt" || fail "could not download checksums.txt from release $tag"

expected=$(awk -v name="$archive" '$2 == name { print $1 }' "$workdir/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt in release $tag has no line for $archive"
[ "$(sha256 "$workdir/$archive")" = "$expected" ] || fail "checksum mismatch for $archive in release $tag: not installing"

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
