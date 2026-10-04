setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}

@test "backing up and checking by hand run the scripts directly, not through systemd" {
  for target in backup-now verify-backup-now; do
    run make -s -n -C "$REPO" "$target"
    [ "$status" -eq 0 ]
    ! echo "$output" | grep -q systemctl || false
  done
  make -s -n -C "$REPO" backup-now | grep -q "scripts/engine-run backup"
  make -s -n -C "$REPO" verify-backup-now | grep -q "scripts/engine-run verify-backup"
}

@test "the timers run the same commands as the make targets" {
  grep -q "scripts/engine-run backup" "$REPO/systemd/media-backup.service"
  grep -q "scripts/engine-run verify-backup" "$REPO/systemd/media-verify.service"
}

@test "the backup targets run the Python programs" {
  make -s -n -C "$REPO" claim-backup-main | grep -q "scripts/engine-run claim-backup-main"
  make -s -n -C "$REPO" unlock-backup | grep -q "scripts/engine-run unlock-backup"
  make -s -n -C "$REPO" restore | grep -q "scripts/engine-run restore"
}

@test "targets that need an installation say where installations are, run outside one" {
  home=$(mktemp -d)
  mkdir -p "$home/trial/engine" "$home/trial/config"
  echo INSTALLATION_NAME=trial > "$home/trial/config/installation.env"
  for target in backup-now verify-backup-now claim-backup-main install-backup-timers media-start urls; do
    run env HOME="$home" make -s -C "$REPO" "$target" CONFIG_DIR="$home/nowhere/config"
    [ "$status" -ne 0 ] || { echo "$target did not refuse"; false; }
    echo "$output" | grep -q "not an installation" || { echo "$target: $output"; false; }
    echo "$output" | grep -qx "  cd $home/trial" || { echo "$target: $output"; false; }
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
  make -s -n -C "$REPO" unlock-backup | grep -q "backup.sops.env.*scripts/engine-run unlock-backup"
  make -s -n -C "$REPO" unlock-backup ALL=1 | grep -q "scripts/engine-run unlock-backup --remove-all"
}

@test "make update installs the pinned tools before checking them" {
  out=$(make -s -n -C "$REPO" update CONFIG_DIR=/nowhere 2>/dev/null)
  [ "$(echo "$out" | grep -n "bootstrap.sh --pinned-tools" | cut -d: -f1)" -lt "$(echo "$out" | grep -n "scripts/check-tools.sh" | cut -d: -f1)" ]
}

@test "make version names the engine running and the version the config pins, and says when they differ" {
  dir=$(mktemp -d)
  mkdir -p "$dir/config"
  echo INSTALLATION_NAME=trial > "$dir/config/installation.env"
  echo ENGINE_VERSION=v0.9.0 > "$dir/config/engine.env"
  run make -s -C "$REPO" version CONFIG_DIR="$dir/config"
  [ "$status" -eq 0 ]
  echo "$output" | grep -q "^engine: $(git -C "$REPO" describe --tags --always)"
  echo "$output" | grep -q "^config pins: v0.9.0"
  echo "$output" | grep -q "make update"
  rm -rf "$dir"
}

@test "the config path the targets use has no .. in it" {
  out=$(make -s -n -C "$REPO" unlock-backup)
  echo "$out" | grep -q "$(cd "$REPO/.." && pwd)/config/secrets/backup.sops.env"
  ! echo "$out" | grep -q "/\.\./" || false
}

@test "make homepage re-renders the landing page, and nothing else" {
  make -s -n -C "$REPO" homepage | grep -q "scripts/engine-run homepage"
  ! make -s -n -C "$REPO" homepage | grep -qE "engine-run update|compose" || false
}

@test "make update runs the Python update" {
  make -s -n -C "$REPO" update | grep -q "scripts/engine-run update"
}

@test "the app, version and image targets run engine-run" {
  make -s -n -C "$REPO" urls | grep -q "scripts/engine-run urls"
  make -s -n -C "$REPO" logins | grep -q "scripts/engine-run logins"
  make -s -n -C "$REPO" version | grep -q "scripts/engine-run version"
  make -s -n -C "$REPO" monitoring-start | grep -q "scripts/engine-run prune-stack-images"
}

@test "the lifecycle targets run engine-run" {
  make -s -n -C "$REPO" create-installation NAME=home | grep -q "scripts/engine-run create-installation home"
  make -s -n -C "$REPO" join-installation NAME=home | grep -q "scripts/engine-run join-installation home"
  make -s -n -C "$REPO" setup-machine | grep -q "scripts/engine-run setup-machine"
  make -s -n -C "$REPO" install-update-timer | grep -q "scripts/engine-run install-timers media-update"
  make -s -n -C "$REPO" install-backup-timers | grep -q "scripts/engine-run install-timers media-backup media-verify"
  make -s -n -C "$REPO" install-download-cleanup-timer | grep -q "scripts/engine-run install-timers media-download-cleanup"
}

@test "make configure runs the Python configure, passing ROTATE on" {
  make -s -n -C "$REPO" configure | grep -qE "scripts/engine-run configure *$"
  make -s -n -C "$REPO" configure ROTATE=sonarr | grep -q "scripts/engine-run configure --rotate sonarr$"
}

@test "the update timer runs the Python scheduled update with the ping key" {
  grep -q "^ExecStart=@SOPS@ exec-env @CONFIG_DIR@/secrets/healthchecks.sops.env 'scripts/engine-run scheduled-update'$" "$REPO/systemd/media-update.service"
}

@test "the stack and installation targets run engine-run, with no lib.sh left" {
  make -s -n -C "$REPO" media-start | grep -q "scripts/engine-run require-installation"
  make -s -n -C "$REPO" media-start | grep -q "scripts/engine-run stack up -d$"
  make -s -n -C "$REPO" media-stop | grep -q "scripts/engine-run stack down$"
  make -s -n -C "$REPO" media-status | grep -q "scripts/engine-run stack ps$"
  make -s -n -C "$REPO" monitoring-start | grep -q "scripts/engine-run monitoring up -d$"
  make -s -n -C "$REPO" monitoring-stop | grep -q "scripts/engine-run monitoring down$"
  make -s -n -C "$REPO" monitoring-status | grep -q "scripts/engine-run monitoring ps$"
  ! grep -rqwF "lib.sh" "$REPO/Makefile" "$REPO/scripts" "$REPO/systemd" || false
}

@test "the targets that run under sops give every sops one command, as sops takes it" {
  STUB_DIR=$(mktemp -d)
  STUB_LOG="$STUB_DIR/calls.log"
  export STUB_LOG
  cat > "$STUB_DIR/sops" <<'STUB'
#!/usr/bin/env bash
[ "$1" = exec-env ] && [ "$#" -eq 3 ] || { echo "error: missing file to decrypt" >&2; exit 1; }
exec sh -c "$3"
STUB
  mkdir -p "$STUB_DIR/work/scripts"
  printf '#!/bin/sh\necho "engine-run $*" >> "$STUB_LOG"\n' > "$STUB_DIR/work/scripts/engine-run"
  chmod +x "$STUB_DIR/sops" "$STUB_DIR/work/scripts/engine-run"
  for target in backup-now verify-backup-now claim-backup-main unlock-backup install-backup-timers restore; do
    recipe=$(make -s -n -C "$REPO" "$target" CONFIG_DIR=/c | grep "sops exec-env")
    (cd "$STUB_DIR/work" && PATH="$STUB_DIR:$PATH" bash -c "$recipe") || { echo "$target: $recipe"; false; }
  done
  [ "$(cat "$STUB_LOG")" = "engine-run backup
engine-run verify-backup
engine-run claim-backup-main
engine-run unlock-backup
engine-run install-timers media-backup media-verify
engine-run restore" ]
  rm -rf "$STUB_DIR"
}

@test "lint runs shellcheck even when the Go lint fails" {
  stubs=$(mktemp -d)
  printf '#!/bin/sh\nexit 1\n' > "$stubs/go"
  printf '#!/bin/sh\necho shellcheck ran >> "%s/calls"\n' "$stubs" > "$stubs/shellcheck"
  chmod +x "$stubs/go" "$stubs/shellcheck"

  run env PATH="$stubs:$PATH" make -s -C "$REPO" lint

  [ "$status" -ne 0 ]
  grep -q "shellcheck ran" "$stubs/calls"
  rm -rf "$stubs"
}

@test "the Go lint and release targets can run prebuilt tools, as CI does" {
  make -s -n -C "$REPO" go-lint GOLANGCI_LINT=golangci-lint | grep -qx "golangci-lint run"
  make -s -n -C "$REPO" release-snapshot GORELEASER=goreleaser | grep -qx "goreleaser release --snapshot --clean"
  make -s -n -C "$REPO" go-lint | grep -qx "go tool -modfile=tools/go.mod golangci-lint run"
}

@test "CI takes the Go tool versions from tools/go.mod" {
  grep -q "go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/golangci/golangci-lint/v2" "$REPO/.github/workflows/test.yml"
  grep -q "go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/goreleaser/goreleaser/v2" "$REPO/.github/workflows/test.yml"
  grep -q "go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/goreleaser/goreleaser/v2" "$REPO/.github/workflows/release.yml"
  ! grep -E "^\s+version: v[0-9]" "$REPO/.github/workflows/test.yml" "$REPO/.github/workflows/release.yml" || false
}
