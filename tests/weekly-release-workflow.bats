setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  WORKFLOW="$REPO/.github/workflows/weekly-release.yml"
}

@test "it runs on Monday mornings and on command, with a dry run" {
  grep -qF "cron: '0 6 * * 1'" "$WORKFLOW"
  grep -q '^  workflow_dispatch:' "$WORKFLOW"
  grep -q '^      dry_run:' "$WORKFLOW"
}

@test "it plans with the script and releases only when main's CI passed on that commit" {
  grep -qF 'scripts/weekly-release-plan.sh' "$WORKFLOW"
  grep -qF 'gh run list --workflow test.yml --commit' "$WORKFLOW"
  grep -qF '"completed success"' "$WORKFLOW"
}

@test "a dry run tags nothing and releases nothing" {
  [ "$(grep -cF "if: needs.plan.outputs.tag != '' && !inputs.dry_run" "$WORKFLOW")" -eq 2 ]
}

@test "it calls the release workflow with the tag and notes" {
  grep -qF 'uses: ./.github/workflows/release.yml' "$WORKFLOW"
  grep -qF 'tag: ${{ needs.plan.outputs.tag }}' "$WORKFLOW"
  grep -qF 'notes: ${{ needs.plan.outputs.notes }}' "$WORKFLOW"
}

@test "a missing test run reads as not run, not null" {
  grep -qF -- "--jq '.[0] // empty | " "$WORKFLOW"
}

@test "it checks the tag against the template's schema before tagging" {
  check=$(grep -n 'scripts/check-release-schema.sh' "$WORKFLOW" | cut -d: -f1)
  tag=$(grep -n 'git tag -a' "$WORKFLOW" | cut -d: -f1)
  [ -n "$check" ]
  [ "$check" -lt "$tag" ]
}
