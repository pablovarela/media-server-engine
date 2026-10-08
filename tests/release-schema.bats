setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  TEMPLATE=$(mktemp -d)
  printf 'config: 0\n' > "$TEMPLATE/config.yml"
}

teardown() {
  rm -rf "$TEMPLATE"
}

@test "a release of the template's schema major passes" {
  run "$REPO/scripts/check-release-schema.sh" v0.9.0 "$TEMPLATE/config.yml"
  [ "$status" -eq 0 ]
}

@test "a release of another major fails before anything is published" {
  run "$REPO/scripts/check-release-schema.sh" v1.0.0 "$TEMPLATE/config.yml"
  [ "$status" -eq 1 ]
  [ "$output" = "v1.0.0 is major 1 but $TEMPLATE/config.yml declares config: 0; a new major bumps both" ]
}

@test "a tag that is not a version fails" {
  run "$REPO/scripts/check-release-schema.sh" nightly "$TEMPLATE/config.yml"
  [ "$status" -eq 1 ]
  [ "$output" = "nightly is not a vMAJOR.MINOR.PATCH tag" ]
}

@test "the release workflow checks the schema before publishing" {
  workflow="$REPO/.github/workflows/release.yml"
  check=$(grep -n 'scripts/check-release-schema.sh "$TAG" config-template/config.yml' "$workflow" | cut -d: -f1)
  publish=$(grep -n 'goreleaser/goreleaser-action' "$workflow" | cut -d: -f1)
  [ -n "$check" ]
  [ "$check" -lt "$publish" ]
}

@test "the release workflow can be called with a tag and notes, and still runs on a pushed tag" {
  workflow="$REPO/.github/workflows/release.yml"
  grep -q '^  workflow_call:' "$workflow"
  grep -q '^      tag:' "$workflow"
  grep -q '^      notes:' "$workflow"
  grep -q '^    tags:' "$workflow"
  grep -qF 'TAG: ${{ inputs.tag || github.ref_name }}' "$workflow"
  grep -qF 'ref: ${{ inputs.tag || github.ref }}' "$workflow"
  grep -qF 'gh release edit "$TAG" --notes-file' "$workflow"
}
