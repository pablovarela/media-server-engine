REPO="$BATS_TEST_DIRNAME/.."

@test "lists the functions that are not fully covered, with the total" {
  run bash -c "printf '%b' 'github.com/pablovarela/media-server-engine/cmd/root.go:25:\t\t\tExecute\t\t\t0.0%\ngithub.com/pablovarela/media-server-engine/cmd/root.go:31:\t\t\trun\t\t\t100.0%\ngithub.com/pablovarela/media-server-engine/internal/selfupdate/selfupdate.go:120:\treplace\t\t\t87.5%\ntotal:\t\t\t\t\t\t(statements)\t\t94.7%\n' | '$REPO/scripts/go-coverage-report.sh'"

  [ "$status" -eq 0 ]
  [ "$output" = "### Go test coverage

Total: 94.7% of statements.

Functions not fully covered:

| Function | Where | Coverage |
|---|---|---|
| \`Execute\` | cmd/root.go:25 | 0.0% |
| \`replace\` | internal/selfupdate/selfupdate.go:120 | 87.5% |" ]
}

@test "says so when every function is covered" {
  run bash -c "printf '%b' 'github.com/pablovarela/media-server-engine/cmd/root.go:31:\t\t\trun\t\t\t100.0%\ntotal:\t\t\t\t\t\t(statements)\t\t100.0%\n' | '$REPO/scripts/go-coverage-report.sh'"

  [ "$status" -eq 0 ]
  [ "$output" = "### Go test coverage

Total: 100.0% of statements.

Every function is fully covered." ]
}
