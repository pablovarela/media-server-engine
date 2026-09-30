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
