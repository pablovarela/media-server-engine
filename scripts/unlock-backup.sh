#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
load_installation

case "${1:-}" in
  "") restic unlock ;;
  --remove-all) restic unlock --remove-all ;;
  *) die "unknown option $1; --remove-all removes every lock" ;;
esac
remaining=$(describe_restic_locks)
if [ -z "$remaining" ]; then
  echo "no locks left on the backup repository"
else
  echo "Locks left, held by restic processes that may still be running:"
  echo "$remaining"
  echo "If none of those machines is running restic now, remove them with: make unlock-backup ALL=1"
fi
