#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

restore_dir=$(mktemp -d "${TMPDIR:-/var/tmp}/media-verify.XXXXXX")

on_exit() {
  local status=$?
  rm -rf "$restore_dir"
  if [ "$status" -ne 0 ]; then
    ping_healthcheck verify /fail
  fi
}

restored_databases() {
  find "$restore_dir" -type f \( -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3' \)
}

is_sqlite_database() {
  [ "$(head -c 15 "$1" | tr -d '\000')" = "SQLite format 3" ]
}

check_database() {
  local name=${1#"$restore_dir"/} result
  result=$(sqlite3 "$1" "PRAGMA integrity_check" 2>&1) || die "cannot check $name: $result"
  [ "$result" = ok ] || die "integrity check failed for $name: $result"
}

trap on_exit EXIT
ping_healthcheck verify /start
"${BACKUP_ROLE_COMMAND:-$(dirname "$0")/backup-role.sh}" is-main ||
  die "another machine is $INSTALLATION_NAME's main; its verification runs there"
restic check --retry-lock 2h
host_filter=$(installation_snapshot_filter)
# shellcheck disable=SC2086
restic restore --retry-lock 2h latest $host_filter --target "$restore_dir" --include '*.db' --include '*.sqlite' --include '*.sqlite3'
databases=$(restored_databases)
[ -n "$databases" ] || die "the latest snapshot holds no databases"
while IFS= read -r database; do
  if is_sqlite_database "$database"; then
    check_database "$database"
  fi
done <<< "$databases"
ping_healthcheck verify
