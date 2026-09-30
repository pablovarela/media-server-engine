load helpers

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/config-template"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$ENGINE_DIR/"
  cp -R "$BATS_TEST_DIRNAME/../config-template/." "$ENGINE_DIR/config-template/"
  make_stub sops '
case $1 in
  decrypt) sed "s/^ENC://" "${@: -1}" ;;
  encrypt) sed "s/^/ENC:/" ;;
esac'
  make_stub gh '[ "$1 $2" != "auth status" ]'
  WHIPTAIL_ANSWERS="$STUB_DIR/whiptail-answers"
  : > "$WHIPTAIL_ANSWERS"
  make_stub whiptail '
case " $* " in *" --msgbox "*) exit 0 ;; esac
answer=$(head -1 "$WHIPTAIL_ANSWERS")
sed -i.bak 1d "$WHIPTAIL_ANSWERS"
[ -n "$answer" ] || { echo "whiptail was asked more than scripted: $*" >&2; exit 255; }
printf "%s" "${answer#*|}" >&2
exit "${answer%%|*}"'
  export WHIPTAIL_ANSWERS CONFIGURE_UI=menus
  export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
  printf 'creation_rules:\n  - path_regex: secrets/\n    age: age1test\n' > "$CONFIG_DIR/.sops.yaml"
}

teardown() {
  teardown_stubs
}

answers() {
  printf '%s\n' "$@" > "$WHIPTAIL_ANSWERS"
}

configure() {
  NAME=testinst "$ENGINE_DIR/scripts/configure.sh" "$@" < /dev/null
}

commits() {
  git -C "$CONFIG_DIR" rev-list --count HEAD 2>/dev/null || echo 0
}

guided_new_installation() {
  answers "0|Europe/London" "1|" "0|admin" "0|/srv/backup/restic" "0|restic-typed" \
    "0|protonvpn" "0|vpn-user" "0|vpn-password" "0|Ireland" "0|" \
    "0|jelly-typed" "0|" "0|portainer-pass-long" "0|en" "0|Save"
}

@test "a new installation is walked through every question, then saved from the menu" {
  guided_new_installation
  run configure
  [ "$status" -eq 0 ]
  grep -qx "TZ=Europe/London" "$CONFIG_DIR/installation.env"
  grep -qx "CONFIG_ON_GITHUB=n" "$CONFIG_DIR/installation.env"
  grep -qx "RESTIC_REPOSITORY=/srv/backup/restic" "$CONFIG_DIR/installation.env"
  grep -qx "ENC:OPENVPN_PASSWORD=vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
  grep -qx "ENC:JELLYFIN_ADMIN_PASSWORD=jelly-typed" "$CONFIG_DIR/secrets/apps.sops.env"
  deluge=$(sed -n 's/^ENC:DELUGE_WEB_PASSWORD=//p' "$CONFIG_DIR/secrets/apps.sops.env")
  [ "${#deluge}" -ge 16 ]
  [ "$(commits)" -eq 1 ]
  ! grep -q "B2 key ID" "$STUB_LOG" || false
}

@test "an existing installation opens on the menu, and one change touches only its file" {
  guided_new_installation
  configure >/dev/null 2>&1
  answers "0|VPN" "0|OPENVPN_PASSWORD" "0|new-vpn-password" "1|" "0|Save"
  run configure
  [ "$status" -eq 0 ]
  [ "$(commits)" -eq 2 ]
  [ "$(git -C "$CONFIG_DIR" show --name-only --format= HEAD)" = "secrets/vpn.sops.env" ]
  grep -qx "ENC:OPENVPN_PASSWORD=new-vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
}

@test "the menus show current values with secrets masked" {
  guided_new_installation
  configure >/dev/null 2>&1
  answers "0|VPN" "1|" "0|Save"
  run configure
  grep -q "OpenVPN user: vpn-user" "$STUB_LOG"
  grep -q "OpenVPN password: set, ends …ord" "$STUB_LOG"
  ! grep -q "vpn-password" "$STUB_LOG" || false
}

@test "discarding an existing installation's changes leaves everything as it was" {
  guided_new_installation
  configure >/dev/null 2>&1
  answers "0|VPN" "0|OPENVPN_USER" "0|someone-else" "1|" "1|"
  run configure
  [ "$status" -eq 0 ]
  [ "$(commits)" -eq 1 ]
  grep -qx "ENC:OPENVPN_USER=vpn-user" "$CONFIG_DIR/secrets/vpn.sops.env"
  echo "$output" | grep -q "Nothing changed"
}

@test "cancelling a new installation's questions stops configure" {
  answers "0|Europe/London" "1|" "255|"
  run configure
  [ "$status" -ne 0 ]
  [ ! -f "$CONFIG_DIR/installation.env" ]
}

@test "a value that fails its check is explained and asked again" {
  answers "0|Europe/London" "1|" "0|admin" "0|/srv/backup/restic" "0|restic-typed" \
    "0|protonvpn" "0|vpn-user" "0|vpn-password" "0|Ireland" "0|" \
    "0|jelly-typed" "0|" "0|short" "0|portainer-pass-long" "0|en" "0|Save"
  run configure
  [ "$status" -eq 0 ]
  grep -q -- "--msgbox Portainer needs at least 12 characters" "$STUB_LOG"
  grep -qx "ENC:PORTAINER_ADMIN_PASSWORD=portainer-pass-long" "$CONFIG_DIR/secrets/apps.sops.env"
}

@test "b2 questions appear once the backup repository is on b2" {
  answers "0|Europe/London" "1|" "0|admin" "0|b2:bucket:restic" "0|0031keyid" "0|K005key" "0|restic-typed" \
    "0|protonvpn" "0|vpn-user" "0|vpn-password" "0|Ireland" "0|" \
    "0|jelly-typed" "0|" "0|portainer-pass-long" "0|en" "0|Save"
  run configure
  [ "$status" -eq 0 ]
  grep -qx "ENC:B2_ACCOUNT_ID=0031keyid" "$CONFIG_DIR/secrets/backup.sops.env"
}

@test "without a terminal or whiptail, configure asks plain questions" {
  unset CONFIGURE_UI
  rm "$STUB_DIR/whiptail"
  run configure < <(printf 'Europe/London\nn\n'; for _ in $(seq 20); do echo; done)
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "Time zone \[Etc/UTC\]"
}
