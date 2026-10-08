# Contributing

## Making a change

1. Branch from an up-to-date `main`, one change per branch.
2. Change the code, its tests and its docs together. A change in behaviour comes with a test that fails without it.
3. Run `make test` (bats, Go tests, shellcheck and golangci-lint; needs `brew install bats-core go shellcheck`; Go's dev tools are pinned in `tools/go.mod` and need no install). With GNU parallel installed (`brew install parallel`), the bats tests run in parallel, one job per CPU; `TEST_JOBS=1` runs them one at a time.
4. Push the branch and open a pull request. Its title becomes the commit on `main`, so write it as a commit subject: short, imperative, saying what changes.
5. CI runs `test`, `lint`, `release-check` and `tool-pins` on the pull request. All of them pass before it is merged.
6. The owner merges, with a squash merge.

`main` is not pushed to directly.

## Rules for the code

- This repository is written as if it were public: no installation names, email addresses, hostnames, bucket names, keys or other personal details in files or commit messages. Installation-specific values live in each installation's config repository.
- Configuration belongs in git. The restic backup is for runtime state (libraries, history, users). When a setting can be declared in a config repository instead of only living in an app's database, declare it there.
- Never commit decrypted secrets, and never print them in output or logs.
- Every service that writes app state runs as uid and gid 1000; a test enforces it.
- Shell scripts are tested with bats, in `tests/`.
- `mse`, the Go engine, lives in `main.go`, `cmd/` (Cobra commands and their flags) and `internal/` (everything else). Commands write to the command's `OutOrStdout()` and `ErrOrStderr()`, and errors reach the user once, as `mse: <error>`. Commands speak through `internal/report` (`report.From(cmd.Context())`): a `Step` for each stage with its result, and `Tool` writers for external output, which reach the terminal only with `--verbose`; `internal/logfile` writes every run to the log.
- Go tests are table-driven, with `Given`, `When` and `Then` structs (`When` left out when every case runs the same action), and use testify. `make go-test` runs every Go test, the repository root's included, and writes `coverage.out` for the code in `cmd/` and `internal/`; pull requests get it as a comment listing the functions not fully covered, read as questions, not a target.
- Interfaces a Go test mocks are narrow and declared where they are used; Mockery generates their mocks (`make go-mocks`, configured in `.mockery.yml`) into `mocks_test.go` files, which are committed. GitHub's API is called through `internal/github`, a small layer over [go-github](https://github.com/google/go-github) that returns the engine's own types. HTTP is mocked with an `http.RoundTripper` that states each expected request and its answer.
- `internal/installation` finds and reads an installation; code takes settings from its `Installation` value, not from the environment. `internal/secrets` decrypts with the sops library. `internal/compose` drives Docker Compose through the Compose SDK (`github.com/docker/compose/v5`), and is the only package importing it. `internal/homepage` draws the landing page and `homepage.env`; `internal/images` and `internal/downloads` hold the housekeeping; files `mse` generates are written through `internal/files`, only when they change. `internal/process` starts external programs and `internal/restic` is the only code that knows restic's arguments and output; `internal/backup` holds the backup commands' flows, and `internal/healthchecks` the pings.
- Go's dev tools are pinned in `tools/go.mod` and run as `go tool -modfile=tools/go.mod <tool>`, so the engine's `go.mod` holds only what `mse` uses. CI reads golangci-lint's and GoReleaser's versions from that file and runs the same Makefile targets with their prebuilt binaries, rather than compiling them on every run.
- Follow-ups and ideas for later are GitHub issues in this repository.

## Renovate

Renovate opens pull requests for the template's images (grouped weekly), the workflow's actions, shellcheck, restic and the Go modules (the engine's and `tools/go.mod`, tidied after each update). Merge them like any other pull request once CI passes.

`mse` pins its restic in `internal/restic/release.env`. Renovate bumps `RESTIC_VERSION`; the pull request fails `tool-pins` until the four `RESTIC_SHA256_<os>_<arch>` lines are copied from the release's `SHA256SUMS`.

## Releases

Releases are tags on `main`, `vMAJOR.MINOR.PATCH`. The major version is also the version of the config format: the template's `config-template/config.yml` holds it as `config:`, and a release that bumps the major bumps that too (the release workflow refuses a tag whose major differs). `mse update` installs any newer release of the same major, so from 1.0.0 a minor or patch release must not need config changes. Until then every 0.x release is the same major: a 0.x release may still change the config, and its notes say what to change.

1. Once CI passes on `main`, tag the commit to release and push the tag: `git tag -a v0.9.0 -m v0.9.0 && git push origin v0.9.0`.
2. The `release` workflow checks that the tag's major is the `config:` in `config-template/config.yml`, then builds `mse` with GoReleaser and publishes the GitHub release with the archives, `checksums.txt` and `install.sh`.
3. Replace the release's body with notes that say what changes for an installation and whether its config needs anything: `gh release edit v0.9.0 --notes-file notes.md`.

An installation picks up the release with `mse update`, which takes the newest release of its major version.

Every Monday at 06:00 UTC the `weekly-release` workflow releases a patch version when everything merged since the last release is a Renovate update or a change to Markdown files only, and main's `test` workflow passed on it. Its notes list the updates by kind. Anything else since the last release, such as a feature or a fix, holds it: the run's summary lists those commits, and they are released by hand with a tag as above. Run it on command from the Actions tab or with `gh workflow run weekly-release.yml`; `-f dry_run=true` reports what it would release without releasing.
