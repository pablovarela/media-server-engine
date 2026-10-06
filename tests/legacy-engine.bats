load helpers

REPO="$BATS_TEST_DIRNAME/.."

@test "nothing mentions the removed engine, its scripts or the installation make targets" {
  cd "$REPO"
  run git grep -nIE \
    -e 'scripts/engine' -e 'engine-run' -e 'bootstrap\.sh' -e 'check-tools\.sh' -e 'tool-versions\.env' \
    -e 'pytest' -e 'uv run' -e 'uv\.lock' -e 'pyproject' -e 'diagnose\.sh' -e 'INSTALL\.md' -e 'engine\.env' -e 'ENGINE_VERSION=' \
    -e 'make (create-installation|join-installation|setup-machine|bootstrap|update|configure|backup-now|verify-backup-now|unlock-backup|restore|claim-backup-main|media-start|media-stop|media-status|urls|logins|homepage|version|pinned-tools|check-tools|install-[a-z-]+)' \
    -- ':!tests/legacy-engine.bats'
  echo "$output"
  [ "$status" -eq 1 ]
}
