setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

@test "backing up and checking by hand run the scripts directly, not through systemd" {
  for target in backup-now verify-backup-now; do
    run make -s -n -C "$REPO" "$target"
    [ "$status" -eq 0 ]
    ! echo "$output" | grep -q systemctl || false
  done
  make -s -n -C "$REPO" backup-now | grep -q "scripts/backup.sh"
  make -s -n -C "$REPO" verify-backup-now | grep -q "scripts/verify-backup.sh"
}

@test "the timers run the same commands as the make targets" {
  grep -q "scripts/backup.sh" "$REPO/systemd/media-backup.service"
  grep -q "scripts/verify-backup.sh" "$REPO/systemd/media-verify.service"
}

@test "targets that need an installation say where installations are, run outside one" {
  home=$(mktemp -d)
  mkdir -p "$home/trial/engine" "$home/trial/config"
  echo INSTALLATION_NAME=trial > "$home/trial/config/installation.env"
  for target in backup-now verify-backup-now claim-backup-main install-backup-timers media-start urls; do
    run env HOME="$home" make -s -C "$REPO" "$target" CONFIG_DIR="$home/nowhere/config"
    [ "$status" -ne 0 ] || { echo "$target did not refuse"; false; }
    echo "$output" | grep -q "not an installation" || { echo "$target: $output"; false; }
    echo "$output" | grep -q "cd $home/trial/engine" || { echo "$target: $output"; false; }
  done
  rm -rf "$home"
}

@test "missed backups, checks and updates run once the machine is back" {
  for timer in media-backup media-verify media-update; do
    grep -qx "Persistent=true" "$REPO/systemd/$timer.timer" || { echo "$timer"; false; }
  done
}

@test "the scheduled update and check start after the scheduled backup" {
  for unit in media-update media-verify; do
    grep -q "^After=.*media-backup.service" "$REPO/systemd/$unit.service" || { echo "$unit"; false; }
  done
}

@test "unlock-backup runs the unlock script with the backup secrets, and passes ALL" {
  make -s -n -C "$REPO" unlock-backup | grep -q "backup.sops.env.*scripts/unlock-backup.sh"
  make -s -n -C "$REPO" unlock-backup ALL=1 | grep -q "scripts/unlock-backup.sh --remove-all"
}

@test "make update installs the pinned tools before checking them" {
  out=$(make -s -n -C "$REPO" update CONFIG_DIR=/nowhere 2>/dev/null)
  [ "$(echo "$out" | grep -n "bootstrap.sh --pinned-tools" | cut -d: -f1)" -lt "$(echo "$out" | grep -n "scripts/check-tools.sh" | cut -d: -f1)" ]
}
