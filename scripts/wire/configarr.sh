#!/usr/bin/env bash
set -euo pipefail
ENGINE_RUN=${ENGINE_RUN:-$(cd "$(dirname "$0")/.." && pwd)/engine-run}

die() {
  echo "$(basename "$0"): $*" >&2
  exit 1
}

hide_passwords() {
  sed -E 's/([Pp]assword[^:]*:).*/\1 (hidden)/'
}

dry_run_flag=()
[ -z "${WIRE_DRY_RUN:-}" ] || dry_run_flag=(-e DRY_RUN=true)

log=$(mktemp)
trap 'rm -f "$log"' EXIT
status=0
COMPOSE_PROFILES=wiring "$ENGINE_RUN" stack run --rm ${dry_run_flag[@]+"${dry_run_flag[@]}"} configarr > "$log" 2>&1 || status=$?
hide_passwords < "$log"
[ "$status" -eq 0 ] || die "configarr exited with $status"
if grep -q '^ERROR' "$log"; then
  die "configarr reported errors: $(grep '^ERROR' "$log" | hide_passwords | head -3 | tr '\n' ' ')"
fi
