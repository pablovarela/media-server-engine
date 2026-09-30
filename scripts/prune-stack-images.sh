#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
cd "$CONFIG_DIR"

readonly COMPOSE_FILES="images.yml images.monitoring.yml compose.override.yml"

pinned_references() {
  # shellcheck disable=SC2086
  { grep -shoE 'image:[[:space:]]*[^[:space:]]+@sha256:[0-9a-z]+' $COMPOSE_FILES || true; } | sed -E 's/image:[[:space:]]*//; s/:[^:@]+@/@/'
}

stack_repositories() {
  pinned_references | cut -d@ -f1 | sort -u
}

outdated_stack_images() {
  local pinned repositories repository tag digest
  pinned=$(pinned_references)
  repositories=$(stack_repositories)
  docker image ls -a --digests --format '{{.Repository}} {{.Tag}} {{.Digest}}' | while read -r repository tag digest; do
    echo "$repositories" | grep -qxF "$repository" || continue
    echo "$pinned" | grep -qxF "$repository@$digest" && continue
    if [ "$tag" = "<none>" ]; then echo "$repository@$digest"; else echo "$repository:$tag"; fi
  done
}

outdated_stack_images | while read -r image; do
  docker image rm "$image" >/dev/null 2>&1 && echo "removed $image" || echo "kept $image (still in use)"
done
