setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

@test "a config never commits decrypted secrets, leftovers of an encryption or Finder files" {
  work=$(mktemp -d)
  cp -R "$REPO/config-template/." "$work/"
  git -C "$work" init -q
  mkdir -p "$work/secrets"
  for ignored in secrets/vpn.env secrets/apps.sops.env.new .DS_Store; do
    touch "$work/$ignored"
    git -C "$work" check-ignore -q "$ignored" || { echo "not ignored: $ignored"; false; }
  done
  touch "$work/secrets/vpn.sops.env"
  ! git -C "$work" check-ignore -q secrets/vpn.sops.env || { echo "an encrypted secret is ignored"; false; }
  rm -rf "$work"
}

@test "the template's README shows the /data paths and the declared library locations" {
  ! grep -nE 'path: /data/(tvshows|movies)|root_folder: /(tv|movies)$|never removed' "$REPO/config-template/README.md" || false
  grep -q 'path: /data/media/tvshows' "$REPO/config-template/README.md"
}
