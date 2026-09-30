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
  export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
  printf 'creation_rules:\n  - path_regex: secrets/\n    age: age1test\n' > "$CONFIG_DIR/.sops.yaml"
}

teardown() {
  teardown_stubs
}

answers_for_new_installation() {
  cat <<'EOF'
testinst
Europe/London
someone
admin
b2:testinst-media-server-backup:restic
0031keyid
K005applicationkey
restic-password-typed
protonvpn
vpn-user
vpn-password
Ireland
hc-ping-key-12345
jellyfin-pass
deluge-pass
portainer-pass-long
EOF
}

enter_on_every_prompt() {
  for _ in $(seq 16); do echo; done
}

configure() {
  "$ENGINE_DIR/scripts/configure.sh" "$@"
}

commits() {
  git -C "$CONFIG_DIR" rev-list --count HEAD 2>/dev/null || echo 0
}

@test "configure fills a new config directory from the answers and commits" {
  run configure < <(answers_for_new_installation)
  [ "$status" -eq 0 ]
  grep -qx "INSTALLATION_NAME=testinst" "$CONFIG_DIR/installation.env"
  grep -qx "TZ=Europe/London" "$CONFIG_DIR/installation.env"
  grep -qx "ENC:OPENVPN_PASSWORD=vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
  grep -qx "ENC:B2_ACCOUNT_KEY=K005applicationkey" "$CONFIG_DIR/secrets/backup.sops.env"
  grep -qx "ENC:HEALTHCHECKS_PING_KEY=hc-ping-key-12345" "$CONFIG_DIR/secrets/healthchecks.sops.env"
  [ -f "$CONFIG_DIR/images.yml" ]
  [ "$(commits)" -eq 1 ]
}

@test "configure keeps secrets out of plain files" {
  run configure < <(answers_for_new_installation)
  ! grep -rq "vpn-password\|K005applicationkey\|jellyfin-pass" "$CONFIG_DIR" --exclude="*.sops.env" --exclude-dir=.git
}

@test "configure generates internal credentials and never shows them" {
  run configure < <(answers_for_new_installation)
  sonarr_key=$(sed -n 's/^ENC:SONARR_API_KEY=//p' "$CONFIG_DIR/secrets/apps.sops.env")
  [[ "$sonarr_key" =~ ^[0-9a-f]{32}$ ]]
  grep -q "^ENC:RADARR_API_KEY=[0-9a-f]\{32\}$" "$CONFIG_DIR/secrets/apps.sops.env"
  grep -q "^ENC:PROWLARR_API_KEY=[0-9a-f]\{32\}$" "$CONFIG_DIR/secrets/apps.sops.env"
  grep -q "^ENC:DELUGE_DAEMON_PASSWORD=." "$CONFIG_DIR/secrets/apps.sops.env"
  ! echo "$output" | grep -q "$sonarr_key"
}

@test "configure run again with only Enter changes nothing and commits nothing" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  before=$(cd "$CONFIG_DIR" && find . -path ./.git -prune -o -type f -exec shasum {} + | sort)
  run configure < <(enter_on_every_prompt)
  [ "$status" -eq 0 ]
  after=$(cd "$CONFIG_DIR" && find . -path ./.git -prune -o -type f -exec shasum {} + | sort)
  [ "$before" = "$after" ]
  [ "$(commits)" -eq 1 ]
  echo "$output" | grep -qi "no changes"
}

@test "configure shows current values as defaults and masks secrets" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  run configure < <(enter_on_every_prompt)
  echo "$output" | grep -q "Installation name \[testinst\]"
  echo "$output" | grep -q "set, ends …key"
  ! echo "$output" | grep -q "K005applicationkey"
}

@test "changing one secret rewrites only its file" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  run configure < <(for i in $(seq 16); do if [ "$i" -eq 11 ]; then echo new-vpn-password; else echo; fi; done)
  [ "$(commits)" -eq 2 ]
  [ "$(git -C "$CONFIG_DIR" show --name-only --format= HEAD)" = "secrets/vpn.sops.env" ]
  grep -qx "ENC:OPENVPN_PASSWORD=new-vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
}

@test "configure --rotate regenerates one internal credential only" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  old_sonarr=$(sed -n 's/^ENC:SONARR_API_KEY=//p' "$CONFIG_DIR/secrets/apps.sops.env")
  old_radarr=$(sed -n 's/^ENC:RADARR_API_KEY=//p' "$CONFIG_DIR/secrets/apps.sops.env")
  run configure --rotate sonarr < <(enter_on_every_prompt)
  [ "$status" -eq 0 ]
  [ "$(sed -n 's/^ENC:SONARR_API_KEY=//p' "$CONFIG_DIR/secrets/apps.sops.env")" != "$old_sonarr" ]
  [ "$(sed -n 's/^ENC:RADARR_API_KEY=//p' "$CONFIG_DIR/secrets/apps.sops.env")" = "$old_radarr" ]
}

@test "configure --rotate refuses an unknown app" {
  run configure --rotate jellyfin < <(enter_on_every_prompt)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "sonarr"
}

@test "configure never pushes" {
  make_stub git 'if [ "$1" = push ]; then echo PUSHED >> "$STUB_LOG"; exit 1; fi; exec /usr/bin/git "$@"'
  run configure < <(answers_for_new_installation)
  ! grep -q PUSHED "$STUB_LOG"
}

@test "configure refuses a config directory without .sops.yaml" {
  rm "$CONFIG_DIR/.sops.yaml"
  run configure < <(answers_for_new_installation)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "create-installation"
}
