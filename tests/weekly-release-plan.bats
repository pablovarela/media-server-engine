bats_require_minimum_version 1.5.0

RENOVATE='29139614+renovate[bot]@users.noreply.github.com'

setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  WORK=$(mktemp -d)
  cd "$WORK"
  git init -q -b main
  git config user.name Person
  git config user.email person@example.com
  git config commit.gpgsign false
  git config tag.gpgsign false
  echo start > README.md
  git add -A && git commit -qm start
  git tag -a v0.1.0 -m v0.1.0
}

teardown() {
  rm -rf "$WORK"
}

commit_as() {
  local email=$1 subject=$2
  shift 2
  for file in "$@"; do
    mkdir -p "$(dirname "$file")"
    echo "$subject" >> "$file"
  done
  git add -A
  GIT_AUTHOR_NAME=author GIT_AUTHOR_EMAIL="$email" git commit -qm "$subject"
}

plan() {
  run "$REPO/scripts/weekly-release-plan.sh"
}

@test "nothing since the tag" {
  plan
  [ "$status" -eq 0 ]
  [ "$output" = "nothing to release since v0.1.0" ]
}

@test "only Renovate's updates: a patch release, grouped" {
  commit_as "$RENOVATE" "Update ghcr.io/x/y Docker tag to v1.2.4 (#10)" config-template/images.yml
  commit_as "$RENOVATE" "Update module github.com/a/b to v1.0.1 (#11)" go.sum
  plan
  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "release v0.1.1" ]
  [[ "$output" == *$'**Images in the config template**\n- Update ghcr.io/x/y Docker tag to v1.2.4 (#10)'* ]]
  [[ "$output" == *$'**Dependencies**\n- Update module github.com/a/b to v1.0.1 (#11)'* ]]
  [[ "$output" != *"**Docs**"* ]]
  [[ "$output" == *"The next nightly update takes v0.1.1, nothing to do."* ]]
}

@test "a Renovate update to the template and other files is a template image update" {
  commit_as "$RENOVATE" "Update template images (#12)" config-template/images.yml renovate.json
  plan
  [[ "$output" == *$'**Images in the config template**\n- Update template images (#12)'* ]]
  [[ "$output" != *"**Dependencies**"* ]]
}

@test "a person's Markdown-only change rides along under Docs" {
  commit_as person@example.com "CONFIG.md: fix a line (#13)" config-template/CONFIG.md docs/BACKUP.md
  commit_as "$RENOVATE" "Update ghcr.io/x/y Docker tag to v1.2.4 (#10)" config-template/images.yml
  plan
  [ "${lines[0]}" = "release v0.1.1" ]
  [[ "$output" == *$'**Docs**\n- CONFIG.md: fix a line (#13)'* ]]
}

@test "a person's code change holds the release" {
  commit_as "$RENOVATE" "Update ghcr.io/x/y Docker tag to v1.2.4 (#10)" config-template/images.yml
  commit_as person@example.com "Add a feature (#14)" cmd/feature.go
  plan
  [ "$status" -eq 0 ]
  [ "${lines[0]}" = "hold: these commits since v0.1.0 need a release by hand:" ]
  [[ "${lines[1]}" == "- "*" Add a feature (#14) (author)" ]]
  [ "${#lines[@]}" -eq 2 ]
}

@test "Markdown and code in one commit holds the release" {
  commit_as person@example.com "Docs and code (#15)" README.md main.go
  plan
  [ "${lines[0]}" = "hold: these commits since v0.1.0 need a release by hand:" ]
}

@test "a merge commit holds the release" {
  git switch -qc side
  commit_as "$RENOVATE" "Update x (#16)" config-template/images.yml
  git switch -q main
  commit_as "$RENOVATE" "Update y (#17)" go.sum
  git merge -q --no-ff side -m "Merge side"
  plan
  [ "${lines[0]}" = "hold: these commits since v0.1.0 need a release by hand:" ]
  [[ "$output" == *"Merge side"* ]]
}

@test "the latest tag is the highest version, not the latest text" {
  git tag -a v0.9.0 -m v0.9.0
  commit_as "$RENOVATE" "Update x (#18)" go.sum
  git tag -a v0.10.0 -m v0.10.0
  commit_as "$RENOVATE" "Update y (#19)" go.sum
  plan
  [ "${lines[0]}" = "release v0.10.1" ]
}

@test "a second run after the release finds nothing" {
  commit_as "$RENOVATE" "Update x (#20)" go.sum
  git tag -a v0.1.1 -m v0.1.1
  plan
  [ "$output" = "nothing to release since v0.1.1" ]
}

@test "no tag to release from: the reason goes to stderr, so a workflow capturing the plan still shows it" {
  git tag -d v0.1.0 >/dev/null
  run --separate-stderr "$REPO/scripts/weekly-release-plan.sh"
  [ "$status" -eq 1 ]
  [ "$output" = "" ]
  [ "$stderr" = "no v* tag to release from" ]
}
