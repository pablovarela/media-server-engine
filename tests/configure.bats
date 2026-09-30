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
  make_stub restic 'exit "${FAKE_RESTIC_STATUS:-10}"'
  export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
  printf 'creation_rules:\n  - path_regex: secrets/\n    age: age1test\n' > "$CONFIG_DIR/.sops.yaml"
}

teardown() {
  teardown_stubs
}

answers_for_new_installation() {
  cat <<'EOF'
Europe/London
github
someone
admin
b2
testinst-media-server-backup
restic
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
  for _ in $(seq 22); do echo; done
}

configure() {
  NAME=testinst "$ENGINE_DIR/scripts/configure.sh" "$@"
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
  echo "$output" | grep -q "Installation: testinst"
  echo "$output" | grep -q "set, ends …key"
  ! echo "$output" | grep -q "K005applicationkey" || false
}

@test "changing one secret rewrites only its file" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  run configure < <(for i in $(seq 22); do if [ "$i" -eq 13 ]; then echo new-vpn-password; else echo; fi; done)
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
  grep -qx "CONFIG_LOCATION=github" "$CONFIG_DIR/installation.env"
  echo "$output" | grep -q "github.com/apps/renovate"
}

@test "a local-only installation never touches a github repo and is not asked for an owner" {
  run configure < <(answers_with '2s/github/local/; 3d')
  [ "$status" -eq 0 ]
  ! grep -q "^gh repo" "$STUB_LOG" || false
  ! git -C "$CONFIG_DIR" remote get-url origin 2>/dev/null || false
  grep -qx "CONFIG_LOCATION=local" "$CONFIG_DIR/installation.env"
  grep -qx "JELLYFIN_ADMIN_USER=admin" "$CONFIG_DIR/installation.env"
  [ "$(commits)" -eq 1 ]
  ! echo "$output" | grep -q "GitHub owner" || false
}

@test "a local config is published when it is switched to github" {
  configure < <(answers_with '2s/github/local/; 3d') >/dev/null 2>&1
  run configure < <(printf '\ngithub\nsomeone\n'; for _ in $(seq 20); do echo; done)
  [ "$status" -eq 0 ]
  grep -q "^gh repo create someone/media-server-config-testinst --private --source . --push$" "$STUB_LOG"
  [ "$(git -C "$CONFIG_DIR" remote get-url origin)" = "git@github.com:someone/media-server-config-testinst.git" ]
}

@test "a config switched to local stops pulling from github and keeps the github repo" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  : > "$STUB_LOG"
  run configure < <(printf '\nlocal\n'; for _ in $(seq 20); do echo; done)
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

@test "configure keeps files of a config that already has the template, before its first run" {
  cp -R "$ENGINE_DIR/config-template/." "$CONFIG_DIR/"
  echo "ENGINE_VERSION=local" > "$CONFIG_DIR/engine.env"
  run configure < <(answers_for_new_installation)
  [ "$status" -eq 0 ]
  grep -qx "ENGINE_VERSION=local" "$CONFIG_DIR/engine.env"
}

@test "the installation name is never asked, since changing it would orphan the installation" {
  run configure < <(answers_for_new_installation)
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -q "Installation name \[" || false
  grep -qx "INSTALLATION_NAME=testinst" "$CONFIG_DIR/installation.env"
  NAME= run "$ENGINE_DIR/scripts/configure.sh" < <(enter_on_every_prompt)
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "Installation: testinst"
}

@test "configure without an installation name refuses" {
  NAME= run "$ENGINE_DIR/scripts/configure.sh" < <(enter_on_every_prompt)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "create-installation"
}

@test "configure outside an installation creates nothing and says where installations are" {
  rm -rf "$CONFIG_DIR"
  NAME= run "$ENGINE_DIR/scripts/configure.sh" < /dev/null
  [ "$status" -ne 0 ]
  [ ! -e "$CONFIG_DIR" ]
  echo "$output" | grep -q "not an installation"
}

@test "a portainer password shorter than 12 characters is asked again" {
  run configure < <(answers_for_new_installation | sed 's/^portainer-pass-long$/short\nportainer-pass-long/')
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "at least 12 characters"
  grep -qx "ENC:PORTAINER_ADMIN_PASSWORD=portainer-pass-long" "$CONFIG_DIR/secrets/apps.sops.env"
}

@test "the backups are a choice between a local folder and backblaze b2" {
  run configure < <(answers_for_new_installation)
  echo "$output" | grep -q "local: A folder on this machine"
  echo "$output" | grep -q "b2: Backblaze B2"
  grep -qx "RESTIC_REPOSITORY=b2:testinst-media-server-backup:restic" "$CONFIG_DIR/installation.env"
}

@test "a local backup folder is asked for, created, and needs no b2 keys" {
  run configure < <(answers_for_new_installation | sed "5s#b2#local#; 6s#.*#~/testinst-backups#; 7d; 8d; 9d")
  [ "$status" -eq 0 ]
  ! echo "$output" | grep -q "B2 key ID" || false
  grep -qx "RESTIC_REPOSITORY=$HOME/testinst-backups" "$CONFIG_DIR/installation.env"
  [ -d "$HOME/testinst-backups" ]
  grep -qx "ENC:RESTIC_PASSWORD=restic-password-typed" "$CONFIG_DIR/secrets/backup.sops.env"
}

@test "a backup folder that cannot be made is explained, and can be kept anyway" {
  mkdir -p "$STUB_DIR/readonly" && chmod 555 "$STUB_DIR/readonly"
  run configure < <(answers_for_new_installation | sed "5s#b2#local#; 6s#.*#$STUB_DIR/readonly/backups#; 7d; 8d; 9d" | sed "7a\\
y")
  chmod 755 "$STUB_DIR/readonly"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "cannot be created or written to"
  grep -qx "RESTIC_REPOSITORY=$STUB_DIR/readonly/backups" "$CONFIG_DIR/installation.env"
}

@test "b2 details that do not open the existing backups are explained and asked again" {
  FAKE_RESTIC_STATUS=12 run configure < <(answers_for_new_installation | sed "10a\\
n\\
b2\\
testinst-media-server-backup\\
restic\\
0031keyid\\
K005applicationkey\\
restic-password-typed\\
y")
  [ "$status" -eq 0 ]
  [ "$(echo "$output" | grep -c "does not open the backups")" -ge 1 ]
}

@test "a time zone that does not exist is asked again" {
  run configure < <(answers_for_new_installation | sed "1s#.*#Mars/Olympus\\
Europe/London#")
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "Mars/Olympus is not a time zone"
  grep -qx "TZ=Europe/London" "$CONFIG_DIR/installation.env"
}

@test "keys added to a secrets file by hand survive configure" {
  configure < <(answers_for_new_installation) >/dev/null 2>&1
  echo "ENC:WIREGUARD_MTU=1320" >> "$CONFIG_DIR/secrets/vpn.sops.env"
  run configure < <(for i in $(seq 22); do if [ "$i" -eq 13 ]; then echo new-vpn-password; else echo; fi; done)
  [ "$status" -eq 0 ]
  grep -qx "ENC:WIREGUARD_MTU=1320" "$CONFIG_DIR/secrets/vpn.sops.env"
  grep -qx "ENC:OPENVPN_PASSWORD=new-vpn-password" "$CONFIG_DIR/secrets/vpn.sops.env"
}

@test "answers that run out while the backup details keep failing stop configure instead of looping" {
  FAKE_RESTIC_STATUS=12 run configure < <(answers_for_new_installation | head -10)
  [ "$status" -ne 0 ]
  echo "$output" | grep -q "answers ran out"
}
