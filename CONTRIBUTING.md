# Contributing

## Making a change

1. Branch from an up-to-date `main`, one change per branch.
2. Change the code, its tests and its docs together. A change in behaviour comes with a test that fails without it.
3. Run `make test` (bats, pytest, Go tests, shellcheck and golangci-lint; needs `brew install bats-core uv go shellcheck`; Go's dev tools are pinned in `tools/go.mod` and need no install). uv installs pytest and its dependencies from `uv.lock` into `.venv/`. With GNU parallel installed (`brew install parallel`), the bats tests run in parallel, one job per CPU; `TEST_JOBS=1` runs them one at a time. `make test-python PYTHON=3.9` runs the Python tests on the Python of Raspberry Pi OS bullseye.
4. Push the branch and open a pull request. Its title becomes the commit on `main`, so write it as a commit subject: short, imperative, saying what changes.
5. CI runs `test`, `python-3-9`, `lint`, `release-check` and `tool-pins` on the pull request. All of them pass before it is merged.
6. The owner merges, with a squash merge.

`main` is not pushed to directly.

## Rules for the code

- This repository is written as if it were public: no installation names, email addresses, hostnames, bucket names, keys or other personal details in files or commit messages. Installation-specific values live in each installation's config repository.
- Configuration belongs in git. The restic backup is for runtime state (libraries, history, users). When a setting can be declared in a config repository instead of only living in an app's database, declare it there.
- Never commit decrypted secrets, and never print them in output or logs.
- Every service that writes app state runs as uid and gid 1000; a test enforces it.
- The engine's Python runs on Python 3.9 and later, the Python of Raspberry Pi OS bullseye.
- Python is tested with pytest, in `tests/python/`, by calling its functions, with HTTP calls mocked at `urllib.request.urlopen`. Shell scripts are tested with bats, in `tests/`, and so is the way they call the Python. `make test-python` prints which lines and branches of the Python no test reaches; the command-line entry points are left to the bats tests.
- The engine's programs are written in the `engine` package, `scripts/engine/`, and started as `scripts/engine-run <command>`. Every command they run goes through `engine.commands`, and tests answer those commands with the `commands` fixture, as the `http` fixture answers requests.
- New logic that is mostly API calls or YAML is written in Python, not shell.
- `mse`, the Go engine, lives in `main.go`, `cmd/` (Cobra commands and their flags) and `internal/` (everything else). Commands write to the command's `OutOrStdout()` and `ErrOrStderr()`, and errors reach the user once, as `mse: <error>`.
- Go tests are table-driven, with `Given`, `When` and `Then` structs (`When` left out when every case runs the same action), and use testify. `make go-test` writes `coverage.out` for `cmd/` and `internal/`; pull requests get it as a comment listing the functions not fully covered, read as questions, not a target.
- Interfaces a Go test mocks are narrow and declared where they are used; Mockery generates their mocks (`make go-mocks`, configured in `.mockery.yml`) into `mocks_test.go` files, which are committed. GitHub's API is called through `internal/github`, a small layer over [go-github](https://github.com/google/go-github) that returns the engine's own types. HTTP is mocked with an `http.RoundTripper` that states each expected request and its answer.
- `internal/installation` finds and reads an installation; code takes settings from its `Installation` value, not from the environment. `internal/secrets` decrypts with the sops library. `internal/compose` drives Docker Compose through the Compose SDK (`github.com/docker/compose/v5`), and is the only package importing it. `internal/homepage` draws the landing page and `homepage.env`; `internal/images` and `internal/downloads` hold the housekeeping; files `mse` generates are written through `internal/files`, only when they change.
- Go's dev tools are pinned in `tools/go.mod` and run as `go tool -modfile=tools/go.mod <tool>`, so the engine's `go.mod` holds only what `mse` uses. CI reads golangci-lint's and GoReleaser's versions from that file and runs the same Makefile targets with their prebuilt binaries, rather than compiling them on every run.
- Follow-ups and ideas for later are GitHub issues in this repository.

## Renovate

Renovate opens pull requests for the template's images (grouped weekly), the workflow's actions, shellcheck, the pinned sops, age and restic, the Python test dependencies in `uv.lock`, and the Go modules (the engine's and `tools/go.mod`, tidied after each update). Merge them like any other pull request once CI passes.

A pull request for sops, age or restic fails `tool-pins` until the SHA256 next to the new version in `scripts/tool-versions.env` is updated: download the new release file named by `scripts/tool-pins.sh`, take its `sha256sum`, commit it to the pull request's branch, and `scripts/check-tool-downloads.sh` passes.

## Releases

Releases are tags on `main`, `vMAJOR.MINOR.PATCH`. The major version is also the version of the config format: the template's `config-template/config.yml` holds it as `config:`, and a release that bumps the major bumps that too (the release workflow refuses a tag whose major differs). `mse update` installs any newer release of the same major, so from 1.0.0 a minor or patch release must not need config changes. Until then every 0.x release is the same major: a 0.x release may still change the config, and its notes say what to change.

1. Once CI passes on `main`, tag the commit to release and push the tag: `git tag -a v0.9.0 -m v0.9.0 && git push origin v0.9.0`.
2. The `release` workflow checks that the tag's major is the `config:` in `config-template/config.yml`, then builds `mse` with GoReleaser and publishes the GitHub release with the archives, `checksums.txt` and `install.sh`.
3. Replace the release's body with notes that say what changes for an installation and whether its config needs anything: `gh release edit v0.9.0 --notes-file notes.md`.

Each installation's config gets a Renovate pull request for the new `ENGINE_VERSION`; merging it is the upgrade (see [docs/UPGRADING.md](docs/UPGRADING.md)).
