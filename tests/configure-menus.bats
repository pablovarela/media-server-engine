load helpers

setup() {
  setup_stubs
  mkdir -p "$ENGINE_DIR/config-template"
  cp -R "$BATS_TEST_DIRNAME/../scripts" "$ENGINE_DIR/"
  cp -R "$BATS_TEST_DIRNAME/../homepage" "$ENGINE_DIR/"
  cp -R "$BATS_TEST_DIRNAME/../config-template/." "$ENGINE_DIR/config-template/"
  make_stub sops '
case $1 in
  decrypt) sed "s/^ENC://" "${@: -1}" ;;
  encrypt) sed "s/^/ENC:/" ;;
esac'
  make_stub gh '[ "$1 $2" != "auth status" ]'
  make_stub restic 'exit "${FAKE_RESTIC_STATUS:-10}"'
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

guided_answers() {
  local folder="$STUB_DIR/backups"
  printf '%s\n' "0|Europe" "0|London" "0|local" "0|admin" "0|local" "0|$folder" "0|restic-typed" \
    "0|protonvpn" "0|vpn-user" "0|vpn-password" "0|Ireland" "0|" "0|" \
    "0|jelly-typed" "0|" "0|portainer-pass-long" "0|en"
}

guided_new_installation() {
  { guided_answers; echo "0|Save"; } > "$WHIPTAIL_ANSWERS"
}

@test "a new installation is walked through every question, then saved from the menu" {
  guided_new_installation
  run configure
  [ "$status" -eq 0 ]
  grep -qx "TZ=Europe/London" "$CONFIG_DIR/installation.env"
  grep -qx "CONFIG_LOCATION=local" "$CONFIG_DIR/installation.env"
  grep -qx "RESTIC_REPOSITORY=$STUB_DIR/backups" "$CONFIG_DIR/installation.env"
  grep -qx "ENC:OPENVPN_PASSWORD=vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
  grep -qx "ENC:JELLYFIN_ADMIN_PASSWORD=jelly-typed" "$CONFIG_DIR/secrets/apps.sops.env"
  deluge=$(sed -n 's/^ENC:DELUGE_WEB_PASSWORD=//p' "$CONFIG_DIR/secrets/apps.sops.env")
  [ "${#deluge}" -ge 16 ]
  [ "$(commits)" -eq 1 ]
  ! grep -q "B2 key ID" "$STUB_LOG" || false
}

@test "the time zone is picked from a region, then a place in it" {
  guided_new_installation
  run configure
  grep -q -- "--menu Time zone" "$STUB_LOG"
  grep -q "Choose a region" "$STUB_LOG"
  grep -q -- "--menu Choose a place in Europe" "$STUB_LOG"
  grep -q " London " "$STUB_LOG"
}

@test "the config location is a choice that says it can be switched later" {
  guided_new_installation
  run configure
  grep -q -- "--menu Where to keep this config" "$STUB_LOG"
  grep -q "You can switch at any time" "$STUB_LOG"
  grep -q "local Only on this machine github A private GitHub repo" "$STUB_LOG"
}

@test "back returns to the previous question" {
  { printf '%s\n' "0|Europe" "0|London" "0|local" "0|admin" "1|" "0|admin2"; guided_answers | tail -n +5; echo "0|Save"; } > "$WHIPTAIL_ANSWERS"
  run configure
  [ "$status" -eq 0 ]
  grep -qx "JELLYFIN_ADMIN_USER=admin2" "$CONFIG_DIR/installation.env"
}

@test "back on the first question asks before stopping, and no carries on" {
  { printf '%s\n' "1|" "1|"; guided_answers; echo "0|Save"; } > "$WHIPTAIL_ANSWERS"
  run configure
  [ "$status" -eq 0 ]
  grep -q "Stop configuring testinst" "$STUB_LOG"
}

@test "escape asks before stopping a new installation, and yes stops it" {
  answers "0|Europe" "0|London" "255|" "0|"
  run configure
  [ "$status" -ne 0 ]
  [ ! -f "$CONFIG_DIR/installation.env" ]
}

@test "every menu says which keys to use" {
  guided_new_installation
  run configure
  grep -q "Tab reaches the buttons" "$STUB_LOG"
}

@test "an existing installation opens on the menu, and one change touches only its file" {
  guided_new_installation
  configure >/dev/null 2>&1
  answers "0|VPN" "0|OPENVPN_PASSWORD" "0|new-vpn-password" "0|Back" "0|Save"
  run configure
  [ "$status" -eq 0 ]
  [ "$(commits)" -eq 2 ]
  [ "$(git -C "$CONFIG_DIR" show --name-only --format= HEAD)" = "secrets/vpn.sops.env" ]
  grep -qx "ENC:OPENVPN_PASSWORD=new-vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
}

@test "the menus show current values with secrets masked, and back and discard as entries" {
  guided_new_installation
  configure >/dev/null 2>&1
  : > "$STUB_LOG"
  answers "0|VPN" "0|Back" "0|Save"
  run configure
  grep -q "OpenVPN user: set, ends …ser" "$STUB_LOG"
  ! grep -q "vpn-user" "$STUB_LOG" || false
  grep -q "OpenVPN password: set, ends …ord" "$STUB_LOG"
  ! grep -q "vpn-password" "$STUB_LOG" || false
  grep -q "Back return to the sections" "$STUB_LOG"
  grep -q "Discard leave without saving" "$STUB_LOG"
}

@test "discarding an existing installation's changes leaves everything as it was" {
  guided_new_installation
  configure >/dev/null 2>&1
  answers "0|VPN" "0|OPENVPN_USER" "0|someone-else" "0|Back" "0|Discard"
  run configure
  [ "$status" -eq 0 ]
  [ "$(commits)" -eq 1 ]
  grep -qx "ENC:OPENVPN_USER=vpn-user" "$CONFIG_DIR/secrets/vpn.sops.env"
  echo "$output" | grep -q "Nothing changed"
}

@test "a value that fails its check is explained and asked again" {
  guided_answers | sed 's/^0|portainer-pass-long$/0|short\n0|portainer-pass-long/' > "$WHIPTAIL_ANSWERS"
  echo "0|Save" >> "$WHIPTAIL_ANSWERS"
  run configure
  [ "$status" -eq 0 ]
  grep -q -- "--msgbox Portainer needs at least 12 characters" "$STUB_LOG"
  grep -qx "ENC:PORTAINER_ADMIN_PASSWORD=portainer-pass-long" "$CONFIG_DIR/secrets/apps.sops.env"
}

@test "b2 details are asked for b2, and rejected ones are offered to keep anyway" {
  FAKE_RESTIC_STATUS=12
  export FAKE_RESTIC_STATUS
  { printf '%s\n' "0|Europe" "0|London" "0|local" "0|admin" "0|b2" "0|bucket" "0|restic" "0|0031keyid" "0|K005key" "0|restic-typed" "0|"
    guided_answers | tail -n +8; echo "0|Save"; } > "$WHIPTAIL_ANSWERS"
  run configure
  [ "$status" -eq 0 ]
  grep -q "does not open the backups already in b2:bucket:restic" "$STUB_LOG"
  grep -qx "RESTIC_REPOSITORY=b2:bucket:restic" "$CONFIG_DIR/installation.env"
  grep -qx "ENC:B2_ACCOUNT_ID=0031keyid" "$CONFIG_DIR/secrets/backup.sops.env"
}

@test "without a terminal or whiptail, configure asks plain questions" {
  unset CONFIGURE_UI
  rm "$STUB_DIR/whiptail"
  run configure
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "Time zone \[Etc/UTC\]"
}

@test "rotating in the menus says what it rotates" {
  guided_new_installation
  configure >/dev/null 2>&1
  : > "$STUB_LOG"
  answers "0|Save"
  run configure --rotate radarr
  [ "$status" -eq 0 ]
  grep -q -- "--msgbox Rotating Radarr's API key" "$STUB_LOG"
  echo "$output" | grep -q "Run make update"
}

@test "the landing page has its own section in the menu, outside the first walk" {
  guided_new_installation
  configure >/dev/null 2>&1
  answers "0|Save"
  run configure
  [ "$status" -eq 0 ]
  grep -q "Landing page" "$STUB_LOG"
}
