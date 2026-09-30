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
  make_stub gh '
case "$1 $2" in
  "auth status") [ -z "${FAKE_GH_LOGGED_OUT:-}" ] ;;
  "repo view") [ -n "${FAKE_REPO_EXISTS:-}" ] ;;
  "repo create") git remote add origin "git@github.com:$3.git" ;;
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
y
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
en
EOF
}

enter_on_every_prompt() {
  for _ in $(seq 18); do echo; done
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
  ! grep -rq "vpn-password\|K005applicationkey\|jellyfin-pass" "$CONFIG_DIR" --exclude="*.sops.env" --exclude-dir=.git || false
}

@test "configure generates internal credentials and never shows them" {
  run configure < <(answers_for_new_installation)
  sonarr_key=$(sed -n 's/^ENC:SONARR_API_KEY=//p' "$CONFIG_DIR/secrets/apps.sops.env")
  [[ "$sonarr_key" =~ ^[0-9a-f]{32}$ ]]
  grep -q "^ENC:RADARR_API_KEY=[0-9a-f]\{32\}$" "$CONFIG_DIR/secrets/apps.sops.env"
  grep -q "^ENC:PROWLARR_API_KEY=[0-9a-f]\{32\}$" "$CONFIG_DIR/secrets/apps.sops.env"
  ! grep -q "DELUGE_DAEMON" "$CONFIG_DIR/secrets/apps.sops.env" || false
  ! echo "$output" | grep -q "$sonarr_key" || false
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
  ! echo "$output" | grep -q "K005applicationkey" || false
}

@test "changing one secret rewrites only its file" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  run configure < <(for i in $(seq 18); do if [ "$i" -eq 12 ]; then echo new-vpn-password; else echo; fi; done)
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

@test "configure --rotate refuses deluge, whose only credential is the typed web password" {
  run configure --rotate deluge < <(enter_on_every_prompt)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "can only rotate: sonarr, radarr, prowlarr$"
}

@test "configure --rotate refuses an unknown app" {
  run configure --rotate jellyfin < <(enter_on_every_prompt)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "sonarr"
}

@test "configure never pushes" {
  make_stub git 'if [ "$1" = push ]; then echo PUSHED >> "$STUB_LOG"; exit 1; fi; exec /usr/bin/git "$@"'
  run configure < <(answers_for_new_installation)
  ! grep -q PUSHED "$STUB_LOG" || false
}

@test "configure refuses a config directory without .sops.yaml" {
  rm "$CONFIG_DIR/.sops.yaml"
  run configure < <(answers_for_new_installation)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "create-installation"
}

answers_with() {
  answers_for_new_installation | sed "$1"
}

@test "a new installation kept on github is published as a private repo after the commit" {
  run configure < <(answers_for_new_installation)
  [ "$status" -eq 0 ]
  grep -q "^gh repo create someone/media-server-config-testinst --private --source . --push$" "$STUB_LOG"
  grep -qx "CONFIG_ON_GITHUB=y" "$CONFIG_DIR/installation.env"
  echo "$output" | grep -q "github.com/apps/renovate"
}

@test "a local-only installation never touches a github repo and is not asked for an owner" {
  run configure < <(answers_with '3s/y/n/; 4d')
  [ "$status" -eq 0 ]
  ! grep -q "^gh repo" "$STUB_LOG" || false
  ! git -C "$CONFIG_DIR" remote get-url origin 2>/dev/null || false
  grep -qx "CONFIG_ON_GITHUB=n" "$CONFIG_DIR/installation.env"
  grep -qx "JELLYFIN_ADMIN_USER=admin" "$CONFIG_DIR/installation.env"
  [ "$(commits)" -eq 1 ]
  ! echo "$output" | grep -q "GitHub owner" || false
}

@test "a local config is published when it is switched to github" {
  configure < <(answers_with '3s/y/n/; 4d') >/dev/null 2>&1
  run configure < <(printf '\n\ny\nsomeone\n'; for _ in $(seq 16); do echo; done)
  [ "$status" -eq 0 ]
  grep -q "^gh repo create someone/media-server-config-testinst --private --source . --push$" "$STUB_LOG"
  [ "$(git -C "$CONFIG_DIR" remote get-url origin)" = "git@github.com:someone/media-server-config-testinst.git" ]
}

@test "a config switched to local stops pulling from github and keeps the github repo" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  : > "$STUB_LOG"
  run configure < <(printf '\n\nn\n'; for _ in $(seq 16); do echo; done)
  [ "$status" -eq 0 ]
  ! git -C "$CONFIG_DIR" remote get-url origin 2>/dev/null || false
  ! grep -q "^gh repo delete" "$STUB_LOG" || false
  echo "$output" | grep -q "local only"
}

@test "publishing refuses a github repo that already exists" {
  FAKE_REPO_EXISTS=1 run configure < <(answers_for_new_installation)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "someone/media-server-config-testinst already exists"
  ! grep -q "^gh repo create" "$STUB_LOG" || false
}

@test "publishing needs the github cli logged in" {
  FAKE_GH_LOGGED_OUT=1 run configure < <(answers_for_new_installation)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "gh auth login"
}

@test "subtitle languages default to english and are written to apps.yml" {
  run configure < <(answers_with '$s/en/en, es/')
  [ "$status" -eq 0 ]
  [ "$(python3 -c 'import sys, yaml; print(yaml.safe_load(open(sys.argv[1]))["bazarr"]["languages"])' "$CONFIG_DIR/apps.yml")" = "['en', 'es']" ]
  run configure < <(enter_on_every_prompt)
  echo "$output" | grep -q "Subtitle languages .*\[en, es\]"
}
