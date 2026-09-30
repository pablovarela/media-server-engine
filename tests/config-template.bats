setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

keys_configure_writes_to_apps_secrets() {
  local internal
  internal=$(sed -n 's/^readonly INTERNAL_CREDENTIALS="\(.*\)"$/\1/p' "$REPO/scripts/configure.sh")
  sed -n 's/^ *write_secret secrets\/apps\.sops\.env //p' "$REPO/scripts/configure.sh" |
    sed "s/\$INTERNAL_CREDENTIALS/$internal/" | tr ' ' '\n'
}

@test "every secret the configarr template uses is one configure writes" {
  known=$(keys_configure_writes_to_apps_secrets)
  [ -n "$known" ]
  for secret in $(grep -o '!secret [A-Z_]*' "$REPO/config-template/configarr/config.yml" | cut -d' ' -f2 | sort -u); do
    echo "$known" | grep -qx "$secret" || { echo "configure does not write $secret"; false; }
  done
}
