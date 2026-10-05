# media-server-engine

[![test](https://github.com/pablovarela/media-server-engine/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/pablovarela/media-server-engine/actions/workflows/test.yml)

A self-hosted media server that you can rebuild on any machine from two things: the name of the installation and its secrets key.

It runs Jellyfin, Sonarr, Radarr, Prowlarr, Bazarr, Deluge (behind a VPN with gluetun and FlareSolverr), Seerr, Maintainerr and Portainer, with Configarr for quality profiles, a landing page with Homepage at `http://<machine>` linking to every app with live summaries, and an optional monitoring stack (Prometheus, Grafana, cAdvisor, node-exporter). The apps are wired to each other automatically: API keys, indexers, download client, libraries, subtitle languages and the links between them.

## How it is split

An installation is three folders side by side in `~/<name>`, with a Makefile there that runs the engine's targets:

| Folder | What it holds | Where it comes from |
|---|---|---|
| `engine/` | this repository: compose files, scripts, timers, tests | a copy of the engine, at the release the config asks for |
| `config/` | the installation's settings, image versions and encrypted secrets | its own git repository, `media-server-config-<name>`, local or on GitHub |
| `data/` | app state, media and downloads | created on the machine; app state is backed up with restic |

Everything an installation declares lives in its config: change it with `make configure` or by editing the files, and `make update` applies it. The engine never holds anything specific to one installation.

## Quick start

On a Raspberry Pi with Raspberry Pi OS (64-bit) or a Mac with Docker:

```
git clone <this repository> media-server-engine
cd media-server-engine
make create-installation NAME=<name>
```

It installs the tools, makes a secrets key (save it in your password manager), asks for the settings, brings the apps up, wires them and prints where everything is. To add another machine to an existing installation, or to rebuild one after losing a machine, use `make join-installation NAME=<name>` instead.

From then on, run make from the installation, `cd ~/<name>` (its Makefile passes every target to the engine):

| Command | Does |
|---|---|
| `make configure` | change settings and secrets (menus with whiptail, plain questions otherwise) |
| `make update` | apply config changes, update images and the engine, wire the apps |
| `make urls`, `make logins` | the apps' addresses, and their logins |
| `make version` | the engine release running, and the one the config pins |
| `make backup-now`, `make verify-backup-now` | back up now, check the backups now |
| `make media-stop`, `make media-start` | stop and start the apps |

`make help` lists every target.

## Documentation

- [Installing](docs/INSTALL.md): creating an installation and adding machines to it.
- [Backups](docs/BACKUP.md): what is backed up, where, when, and which machine does it.
- [Restoring](docs/RESTORE.md): rebuilding a machine or rolling back app state.
- [Upgrading](docs/UPGRADING.md): engine releases, image updates and going back.
- Each config repository has a README and a CONFIG.md describing its files.

## Installing mse

`mse` is the engine's Go binary. It prints its version (`mse version`), updates itself (`mse update`), runs an installation's containers (`mse stack`, `mse monitoring`), draws its landing page (`mse homepage`), prints its addresses and logins (`mse urls`, `mse logins`), and cleans up old images and executable downloads (`mse prune-stack-images`, `mse remove-executable-downloads`). The Makefile runs the Python engine for everything else. It is built for Linux and macOS on amd64 and arm64, and installed from a GitHub release. The repository is private, so installing needs a token that can read it. Where gh is logged in:

    curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
      https://raw.githubusercontent.com/pablovarela/media-server-engine/main/install.sh | sh

Elsewhere, such as a Raspberry Pi without gh, export a token (a fine-grained token with read access to the repository's contents is enough):

    export GITHUB_TOKEN=<token>
    curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" \
      https://raw.githubusercontent.com/pablovarela/media-server-engine/main/install.sh | sh

`install.sh` takes the token from `GITHUB_TOKEN`, or from `gh auth token` when gh is installed. It installs the latest release into `~/.local/bin`. Settings for `install.sh` go on its side of the pipe: `… | MSE_VERSION=v0.7.0 sh` picks a release, and `MSE_INSTALL_DIR` another directory. It checks the archive against the release's `checksums.txt` before installing, and leaves the installed `mse` in place when the download or the check fails.

`mse update` replaces the installed `mse` with the newest release of its major version, after checking it against `checksums.txt` and running it once; it takes the token the same way as `install.sh`. A newer major version can need config changes, so `mse update` only says it is available; `mse update --force` installs it.

From a clone, `make go-build` builds `dist/mse` for the machine it runs on.

`mse` keeps an installation in the XDG base directories: its config (the clone of the config repository) in `~/.config/mse/<name>`, its data in `~/.local/share/mse/<name>`, and the files it generates, decrypted secrets included, in `~/.local/state/mse/<name>`. `XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` move them. With one installation on the machine `mse` uses it; with several, `--installation <name>` or `MSE_INSTALLATION` chooses. It reads the age key from `SOPS_AGE_KEY_FILE`, `SOPS_AGE_KEY` or `SOPS_AGE_KEY_CMD`, else from `~/.config/sops/age/keys.txt` (under `XDG_CONFIG_HOME`), and talks to the Docker daemon directly: no `sops` or `docker compose` command is needed.

`mse stack up` and `mse stack restart` draw the landing page into the state directory before starting the containers, from the config's `homepage/` files over the engine's default page.

`mse stack` and `mse monitoring` mount the decrypted secrets and the engine's files from the state directory, where the Makefile mounts them from the engine checkout. `mse stack up` therefore recreates containers the Makefile started, and the other way round: manage an installation's containers with one of them.

## Developing

`make test` runs the bats, Python and Go tests, shellcheck and golangci-lint; GitHub Actions runs them on every push and pull request, builds every release archive with GoReleaser, and checks that the pinned sops, age and restic downloads match their checksums. Renovate opens pull requests for the template's images, the pinned tools, shellcheck and the actions; a tool update needs its SHA256 in `scripts/tool-versions.env` updated by hand before that check passes. See [CONTRIBUTING.md](CONTRIBUTING.md) for how changes are made and released.
