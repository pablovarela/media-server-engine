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

Media and downloads live together under `data/data/`, mounted as `/data` in Deluge, Radarr, Sonarr, Bazarr and Jellyfin: downloads in `/data/downloads`, films in `/data/media/movies`, series in `/data/media/tvshows`. One mount lets Radarr and Sonarr import a finished download as a hardlink, so it is neither written again nor stored twice; Linux refuses a hardlink across two mounts, even on one disk.

Everything an installation declares lives in its config: change it with `mse configure` or by editing the files, and `make update` applies it. The engine never holds anything specific to one installation.

## Quick start

On a Raspberry Pi with Raspberry Pi OS (64-bit), another Debian machine or a Mac with Docker, install `mse` (see [Installing mse](#installing-mse)), then:

```
mse check-machine
mse create <name>
```

`mse check-machine` says what the machine still needs; run it until it says the machine is ready. `mse create` makes the installation's secrets key (save it in your password manager), asks for the settings, pushes the config to a new private GitHub repository, makes this machine the main, brings the apps up and wires them. To add another machine to an existing installation, or to rebuild one after losing a machine, use `make join-installation NAME=<name>` from a clone of this repository.

From then on, run make from the installation, `cd ~/<name>` (its Makefile passes every target to the engine):

| Command | Does |
|---|---|
| `mse configure` | change settings and secrets from a menu, then commit and push them |
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

`mse` is the engine's Go binary. It prints its version (`mse version`), creates an installation (`mse create`), updates itself and the config (`mse update`), applies the config (`mse apply`), runs an installation's containers (`mse stack`, `mse monitoring`), draws its landing page (`mse homepage`), prints its addresses and logins (`mse urls`, `mse logins`), cleans up old images and executable downloads (`mse prune-stack-images`, `mse remove-executable-downloads`), and backs up and restores it (`mse backup`, `mse verify-backup`, `mse restore`, `mse backup-role`, `mse claim-backup-main`, `mse unlock-backup`). The Makefile runs the Python engine for everything else. It is built for Linux and macOS on amd64 and arm64, and installed from a GitHub release. The repository is private, so installing needs a token that can read it. Where gh is logged in:

    curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
      https://raw.githubusercontent.com/pablovarela/media-server-engine/main/install.sh | sh

Elsewhere, such as a Raspberry Pi without gh, export a token (a fine-grained token with read access to the repository's contents is enough):

    export GITHUB_TOKEN=<token>
    curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" \
      https://raw.githubusercontent.com/pablovarela/media-server-engine/main/install.sh | sh

`install.sh` takes the token from `GITHUB_TOKEN`, or from `gh auth token` when gh is installed. It installs the latest release into `~/.local/bin`. Settings for `install.sh` go on its side of the pipe: `… | MSE_VERSION=v0.7.0 sh` picks a release, and `MSE_INSTALL_DIR` another directory. It checks the archive against the release's `checksums.txt` before installing, and leaves the installed `mse` in place when the download or the check fails.

Then check the machine with `mse check-machine`. It goes through what an installation needs, in order: git; gh logged in with its token on disk, which the unattended update needs (a `GITHUB_TOKEN` in the shell isn't enough); git using gh for github.com (`gh auth setup-git`), which is how the config is fetched and pushed; Docker, the user in the `docker` group and Docker answering this session; restic; lingering and a user manager that has the docker group (under systemd); and the stack's ports, free or held by the stack itself. It changes nothing: each line is ✓, ✗ with the command that fixes it, or – when it waits for an earlier one. Run it again until it says `This machine is ready.`; it exits 1 until then.

`mse create <name>` creates a new installation on a machine that `mse check-machine` passes, and runs those checks first. It makes a new age key, adds it to `~/.config/sops/age/keys.txt` (or `SOPS_AGE_KEY_FILE`; it refuses while `SOPS_AGE_KEY` or `SOPS_AGE_KEY_CMD` is set) and shows it once: save it, since the name and the key rebuild the installation anywhere. It writes the config from the template into `~/.config/mse/<name>`, asks every configure section in turn, commits, creates the private repository `media-server-config-<name>` under the user gh is logged in as (`--owner <org>` for an organisation) and pushes. Then it makes this machine the main (`mse claim-backup-main`, which creates the backup repository and backs up the empty data folder) and applies the config (`mse apply`). The name is lowercase letters, digits and `-`, starting with a letter. It stops before writing anything when the name is taken here or on GitHub, or when this machine already runs another installation (a machine runs one installation's stack). When something else on the machine uses port 80, `--homepage-port <port>` puts the landing page on another port, for the checks and in the settings. If it fails or you quit before the repository is created, it removes what it wrote, the key included, so you can run it again; after that, it says which commands finish the job.

`mse update` fast-forwards the config from its remote, then replaces the installed `mse` with the newest release of its major version, after checking it against `checksums.txt` and running it once; it takes the token the same way as `install.sh`. It refuses to run while the config has changes that are not committed. A newer major version can need config changes, so `mse update` only says it is available; `mse update --force` installs it.

`mse configure` changes the installation's settings and secrets from a menu in the terminal: General (time zone, Jellyfin admin user, homepage port and host names), Backups, VPN, Healthchecks, App logins, and new random internal API keys for Sonarr, Radarr and Prowlarr. It updates the config first and refuses while the config has uncommitted changes. Secret fields are masked: Enter keeps the current value, and `-` removes an optional one (an OpenVPN login for WireGuard, a Healthchecks key, a B2 key). Saving shows what changed (secrets only as "changed"), writes `installation.env` and re-encrypts only the secret files that changed with the config's `.sops.yaml`, commits and pushes. Nothing is applied on the machine it runs on: it ends by saying so, with `mse apply` to apply it now; every machine applies it at its next nightly update. Lines it doesn't manage, comments included, stay as they are. It needs a terminal and a git name and email for the config.

`mse apply` applies the config on disk to the machine, uncommitted changes included: it writes the secrets, draws the landing page, sets up the healthchecks.io checks (under systemd, with `HEALTHCHECKS_MANAGE_KEY`), pulls the stack's images (trying again when a registry limits requests), starts the stack, puts Prowlarr, FlareSolverr and Deluge back on gluetun's network if they lost it, wires the apps as the config's `apps.yml` and `prowlarr.yml` declare (Prowlarr, Jellyfin, Sonarr's and Radarr's library updates, Deluge, Configarr, Seerr, Bazarr, Maintainerr), reloads Homepage and removes outdated images. A failed pull stops it before anything restarts. Each change the wiring makes is one line; an app that fails or doesn't answer within five minutes is named, the others are still wired, and the command fails. `mse update --apply` does both: it waits for a running backup, updates, then applies with the updated `mse`, and reports to healthchecks.io as the update check; it is what the nightly timer runs.

`mse apply` also sets up the systemd timers that run the installation unattended, as its last step, even when an earlier step failed, so the nightly update is there to try again (it needs a user session, as from an ssh login or the timers themselves): `mse update --apply` daily at 05:00 and `mse remove-executable-downloads` every 15 minutes, and on the installation's main `mse backup` at 04:30 and `mse verify-backup` on Sundays at 05:30. They are user units in `~/.config/systemd/user`, named after the installation (`mse-<name>-update` and so on), so `systemctl --user list-timers` lists them and `journalctl --user-unit mse-<name>-update.service` shows a run. They run with nobody logged in once lingering is on for the user, which takes `sudo loginctl enable-linger <user>` once; until then `mse apply` says so and leaves them out. Add the user to the `docker` group before turning lingering on: the user's systemd keeps the groups it started with until it restarts, and `mse apply` warns when it started without `docker` (`sudo systemctl restart user@<uid>`, or a reboot, fixes it). Which machine is the main comes from the backup repository, as for `mse backup`; a machine another one backs up for has its backup timers removed. The units call the installed `mse` by its full path and carry `XDG_*_HOME` and `SOPS_AGE_KEY_FILE`/`SOPS_AGE_KEY_CMD` as the shell running `mse apply` has them (it warns when one the timers had is missing); a dev build sets up no timers. The nightly update needs a GitHub token with nobody logged in, so `gh auth login` must have kept its token in `~/.config/gh/hosts.yml` (where it goes without a keyring); `mse apply` warns when it hasn't.

From a clone, `make go-build` builds `dist/mse` for the machine it runs on.

`mse` keeps an installation in the XDG base directories: its config (the clone of the config repository) in `~/.config/mse/<name>`, its data in `~/.local/share/mse/<name>`, and the files it generates, decrypted secrets included, in `~/.local/state/mse/<name>`. `XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` move them. With one installation on the machine `mse` uses it; with several, `--installation <name>` or `MSE_INSTALLATION` chooses. It reads the age key from `SOPS_AGE_KEY_FILE`, `SOPS_AGE_KEY` or `SOPS_AGE_KEY_CMD`, else from `~/.config/sops/age/keys.txt` (under `XDG_CONFIG_HOME`), and talks to the Docker daemon directly: no `sops` or `docker compose` command is needed.

When the config pins a `homepage` image, `mse stack up` and `mse stack restart` draw the landing page into the state directory before starting the containers, from the config's `homepage/` files over the engine's default page, then reload Homepage so it shows it. A page that cannot be drawn is reported and the containers start anyway. `mse homepage` redraws the page on its own.

`mse stack up --wait` returns once every container is running, and healthy when it has a healthcheck (`--wait-timeout`, 5 minutes by default). `mse stack logs -f [service...]` follows the logs, and `--tail N` limits them to the last lines. On a terminal `mse` colours its output; `NO_COLOR` turns that off. Commands say what they are doing one step per line; `--verbose` (`-v`) also shows the output of restic and Compose. Every run is logged in full, whatever the flags, to `~/.local/state/mse/<name>/logs/mse.log` (`~/.local/state/mse/mse.log` before an installation is known), rotated at 10 MB with five compressed files kept; each line carries the command and a run id, so `grep 'backup\[3f9a2c\]'` gives one run.

The backup commands run `restic`, which must be on `PATH`, with the repository from `RESTIC_REPOSITORY` in the config's `installation.env` and its credentials from `secrets/backup.sops.env`. Only the installation's main backs up: the machine that made the latest snapshot (`mse backup-role` says which; `mse claim-backup-main` takes over). `mse backup` and `mse verify-backup` report to healthchecks.io themselves when the config has a ping key, so a run by hand counts like a timer run. A backup runs restic from the data directory with symlinks resolved, so a data directory linked to another path backs up under the same paths as before.

`mse stack` and `mse monitoring` mount the decrypted secrets and the engine's files from the state directory, where the Makefile mounts them from the engine checkout. `mse stack up` therefore recreates containers the Makefile started, and the other way round: manage an installation's containers with one of them.

## Developing

`make test` runs the bats, Python and Go tests, shellcheck and golangci-lint; GitHub Actions runs them on every push and pull request, builds every release archive with GoReleaser, and checks that the pinned sops, age and restic downloads match their checksums. Renovate opens pull requests for the template's images, the pinned tools, shellcheck and the actions; a tool update needs its SHA256 in `scripts/tool-versions.env` updated by hand before that check passes. See [CONTRIBUTING.md](CONTRIBUTING.md) for how changes are made and released.
