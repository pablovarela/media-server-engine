#!/usr/bin/env bash
set -euo pipefail

renovate='29139614+renovate[bot]@users.noreply.github.com'

latest=$(git tag --list 'v*' --sort=-version:refname | head -n 1)
if [ -z "$latest" ]; then
  echo "no v* tag to release from" >&2
  exit 1
fi

commits=$(git rev-list --reverse "$latest"..HEAD)
if [ -z "$commits" ]; then
  echo "nothing to release since $latest"
  exit 0
fi

images=""
dependencies=""
docs=""
held=""
for commit in $commits; do
  subject=$(git log -1 --format=%s "$commit")
  email=$(git log -1 --format=%ae "$commit")
  parents=$(git log -1 --format=%P "$commit" | wc -w)
  files=$(git diff-tree --no-commit-id --name-only -r "$commit")
  if [ "$parents" -gt 1 ]; then
    held+="- $(git log -1 --format='%h %s (%an)' "$commit")"$'\n'
  elif [ "$email" = "$renovate" ] && grep -q '^config-template/' <<<"$files"; then
    images+="- $subject"$'\n'
  elif [ "$email" = "$renovate" ]; then
    dependencies+="- $subject"$'\n'
  elif [ -n "$files" ] && ! grep -qv '\.md$' <<<"$files"; then
    docs+="- $subject"$'\n'
  else
    held+="- $(git log -1 --format='%h %s (%an)' "$commit")"$'\n'
  fi
done

if [ -n "$held" ]; then
  echo "hold: these commits since $latest need a release by hand:"
  printf '%s' "$held"
  exit 0
fi

IFS=. read -r major minor patch <<<"${latest#v}"
next="v$major.$minor.$((patch + 1))"

group() {
  if [ -n "$2" ]; then
    printf '\n**%s**\n%s' "$1" "$2"
  fi
}

echo "release $next"
echo
echo "Weekly patch release: updates merged since $latest."
group "Images in the config template" "$images"
group "Dependencies" "$dependencies"
group "Docs" "$docs"
echo
echo "## What changes for an installation"
echo
echo "The next nightly update takes $next, nothing to do. A new installation starts from the updated template."
