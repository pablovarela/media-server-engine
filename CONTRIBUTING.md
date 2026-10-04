# Contributing

## Making a change

1. Branch from an up-to-date `main`, one change per branch.
2. Change the code, its tests and its docs together. A change in behaviour comes with a test that fails without it.
3. Run `make test` (bats, pytest and shellcheck; needs `brew install bats-core uv shellcheck`). uv installs pytest and its dependencies from `uv.lock` into `.venv/`. With GNU parallel installed (`brew install parallel`), the bats tests run in parallel, one job per CPU; `TEST_JOBS=1` runs them one at a time. `make test-python PYTHON=3.9` runs the Python tests on the Python of Raspberry Pi OS bullseye.
4. Push the branch and open a pull request. Its title becomes the commit on `main`, so write it as a commit subject: short, imperative, saying what changes.
5. CI runs `test`, `python-3-9`, `lint` and `tool-pins` on the pull request. All of them pass before it is merged.
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
- Follow-ups and ideas for later are GitHub issues in this repository.

## Renovate

Renovate opens pull requests for the template's images (grouped weekly), the workflow's actions, shellcheck, the pinned sops, age and restic, and the Python test dependencies in `uv.lock`. Merge them like any other pull request once CI passes.

A pull request for sops, age or restic fails `tool-pins` until the SHA256 next to the new version in `scripts/tool-versions.env` is updated: download the new release file named by `scripts/tool-pins.sh`, take its `sha256sum`, commit it to the pull request's branch, and `scripts/check-tool-downloads.sh` passes.

## Releases

Releases are tags on `main`, `vMAJOR.MINOR.PATCH`. Until 1.0.0 a minor release may change the config format; its release notes say what an installation has to change.

1. In a pull request, set `config-template/engine.env` to the new version.
2. Once it is merged and CI passes on `main`, tag that commit and push the tag: `git tag -a v0.2.0 -m v0.2.0 && git push origin v0.2.0`.
3. Create the GitHub release with notes that say what changes for an installation, and whether its config needs anything.

Each installation's config gets a Renovate pull request for the new `ENGINE_VERSION`; merging it is the upgrade (see [docs/UPGRADING.md](docs/UPGRADING.md)).
