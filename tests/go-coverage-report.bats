REPO="$BATS_TEST_DIRNAME/.."

M=github.com/pablovarela/media-server-engine

@test "lists the packages that are not fully covered, lowest first, with the total" {
  printf '%s\n' "mode: set" \
    "$M/cmd/root.go:10.2,12.3 3 0" \
    "$M/cmd/root.go:13.2,14.3 1 1" \
    "$M/internal/process/run.go:5.1,6.2 2 1" \
    "$M/internal/secrets/sops.go:1.1,2.2 4 1" \
    "$M/internal/secrets/sops.go:3.1,4.2 1 0" \
    "$M/internal/secrets/sops.go:3.1,4.2 1 1" \
    "$M/internal/secrets/sops.go:5.1,6.2 2 0" > "$BATS_TEST_TMPDIR/coverage.out"

  run "$REPO/scripts/go-coverage-report.sh" "$BATS_TEST_TMPDIR/coverage.out"

  [ "$status" -eq 0 ]
  [ "$output" = "### Go test coverage

Total: 61.5% of statements.

Packages not fully covered:

| Package | Coverage | Not covered |
|---|---|---|
| cmd | 25.0% | 3 of 4 |
| internal/secrets | 71.4% | 2 of 7 |" ]
}

@test "says so when every package is covered" {
  printf '%s\n' "mode: set" "$M/cmd/root.go:13.2,14.3 1 1" > "$BATS_TEST_TMPDIR/coverage.out"

  run "$REPO/scripts/go-coverage-report.sh" "$BATS_TEST_TMPDIR/coverage.out"

  [ "$status" -eq 0 ]
  [ "$output" = "### Go test coverage

Total: 100.0% of statements.

Every package is fully covered." ]
}
