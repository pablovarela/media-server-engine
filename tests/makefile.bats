setup() {
  REPO="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
}


@test "lint runs shellcheck even when the Go lint fails" {
  stubs=$(mktemp -d)
  printf '#!/bin/sh\nexit 1\n' > "$stubs/go"
  printf '#!/bin/sh\necho shellcheck ran >> "%s/calls"\n' "$stubs" > "$stubs/shellcheck"
  chmod +x "$stubs/go" "$stubs/shellcheck"

  run env PATH="$stubs:$PATH" make -s -C "$REPO" lint

  [ "$status" -ne 0 ]
  grep -q "shellcheck ran" "$stubs/calls"
  rm -rf "$stubs"
}

@test "the Go lint and release targets can run prebuilt tools, as CI does" {
  make -s -n -C "$REPO" go-lint GOLANGCI_LINT=golangci-lint | grep -qx "golangci-lint run"
  make -s -n -C "$REPO" release-snapshot GORELEASER=goreleaser | grep -qx "goreleaser release --snapshot --clean"
  make -s -n -C "$REPO" go-lint | grep -qx "go tool -modfile=tools/go.mod golangci-lint run"
}

@test "CI takes the Go tool versions from tools/go.mod" {
  grep -q "go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/golangci/golangci-lint/v2" "$REPO/.github/workflows/test.yml"
  grep -q "go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/goreleaser/goreleaser/v2" "$REPO/.github/workflows/test.yml"
  grep -q "go list -modfile=tools/go.mod -m -f '{{.Version}}' github.com/goreleaser/goreleaser/v2" "$REPO/.github/workflows/release.yml"
  ! grep -E "^\s+version: v[0-9]" "$REPO/.github/workflows/test.yml" "$REPO/.github/workflows/release.yml" || false
}
