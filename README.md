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

`mse` is the engine's Go binary, replacing `scripts/engine-run` command by command. It is built for Linux and macOS on amd64 and arm64, and installed from a GitHub release. The repository is private, so installing needs a token that can read it:

    curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
      https://raw.githubusercontent.com/pablovarela/media-server-engine/main/install.sh | sh

`install.sh` takes the token from `GITHUB_TOKEN`, or from `gh auth token` when gh is installed. It installs the latest release into `~/.local/bin`; `MSE_VERSION=v0.7.0` picks a release and `MSE_INSTALL_DIR` another directory. It checks the archive against the release's `checksums.txt` before installing, and leaves the installed `mse` in place when anything fails.

From a clone, `make go-build` builds `dist/mse` for the machine it runs on.

## Developing

`make test` runs the bats, Python and Go tests, shellcheck and golangci-lint; GitHub Actions runs them on every push and pull request, builds every release archive with GoReleaser, and checks that the pinned sops, age and restic downloads match their checksums. Renovate opens pull requests for the template's images, the pinned tools, shellcheck and the actions; a tool update needs its SHA256 in `scripts/tool-versions.env` updated by hand before that check passes. See [CONTRIBUTING.md](CONTRIBUTING.md) for how changes are made and released.
