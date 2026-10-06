load helpers

REPO="$BATS_TEST_DIRNAME/.."

@test "nothing mentions the removed Python engine, its scripts or the installation make targets" {
  cd "$REPO"
  run git grep -nIE \
    -e 'scripts/engine' -e 'engine-run' -e 'bootstrap\.sh' -e 'check-tools\.sh' -e 'tool-versions\.env' \
    -e 'pytest' -e 'uv run' -e 'uv\.lock' \
    -e 'make (create-installation|join-installation|setup-machine|bootstrap|update|configure|backup-now|verify-backup-now|unlock-backup|restore|claim-backup-main|media-start|media-stop|media-status|urls|logins|homepage|version|pinned-tools|check-tools|install-[a-z-]+)' \
    -- ':!tests/no-python-engine.bats'
  echo "$output"
  [ "$status" -eq 1 ]
}
