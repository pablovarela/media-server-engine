# media-server-engine

> [!CAUTION]
> **This is a personal project, published only so its CI runs for free. It is not meant for anyone else to use.**
> It is HIGHLY unstable: breaking changes are not just very likely, they are almost GUARANTEED until v1.0.0 is released, if ever. There is no support, no documentation promise and no roadmap. Issues and pull requests are not accepted and will be closed without reply.
>
> You're very welcome to read the code, borrow ideas or fork it and make it your own. I just can't look after it for anyone else. Thanks for understanding, and happy self-hosting.

[![test](https://github.com/pablovarela/media-server-engine/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/pablovarela/media-server-engine/actions/workflows/test.yml)

A self-hosted media server that can be rebuilt on any machine from two things: the installation's name and its secrets key.

## What it runs

- **Jellyfin** to watch, **Seerr** to ask for films and series.
- **Sonarr**, **Radarr**, **Prowlarr** and **Bazarr** to find them, with **Configarr** for quality profiles.
- **Deluge** to download, behind a VPN with **gluetun** and **FlareSolverr**.
- **Maintainerr** to clear out what nobody watches, **Portainer** to look at the containers.
- A landing page with **Homepage** at `http://<machine>`, linking to every app with live summaries.
- Optionally, monitoring with **Prometheus**, **Grafana**, **cAdvisor** and **node-exporter**.

The apps are wired to each other automatically: API keys, indexers, the download client, libraries, subtitle languages and the links between them.

## How it works

- **One binary:** `mse` does everything: set up, configure, apply, update, back up and restore.
- **Config in git:** each installation's settings, image versions and encrypted secrets live in their own private repository, `media-server-config-<name>`. The engine holds nothing specific to one installation.
- **Data on the machine:** app state, media and downloads. App state is backed up with restic; media isn't.
- **One installation per machine.** Several machines can run the same installation: one is the main and backs up, the others are secondaries.
- **Unattended:** systemd timers update and apply the config every night, and the main backs up.

## Getting started

```
curl -fsSL -H "Authorization: Bearer $(gh auth token)" \
  https://raw.githubusercontent.com/pablovarela/media-server-engine/main/install.sh | sh
mse check-machine
```

Run `mse check-machine` until it says the machine is ready; each missing piece comes with the command that fixes it. Then:

| To | Run |
|---|---|
| make a new installation | `mse create <name>` |
| add this machine to an existing one, or rebuild after losing a machine | `mse join <name>` |

`mse create` shows the new secrets key once: save it in a password manager.

## Everyday commands

| Command | Does |
|---|---|
| `mse configure` | change settings and secrets from a menu, then commit and push them |
| `mse apply` | apply the config to this machine: secrets, images, containers, wiring, timers |
| `mse update --apply` | update the config and `mse`, then apply (the nightly timer runs it) |
| `mse urls`, `mse logins` | the apps' addresses, and their logins |
| `mse stack down`, `mse stack up` | stop and start the apps |
| `mse backup`, `mse verify-backup` | back up now, check the backups now |
| `mse backup-role` | which machine is the main |
| `mse version` | the `mse` release running |

`mse help` lists every command.

## Where things live

| What | Where |
|---|---|
| Config, a clone of `media-server-config-<name>` | `~/.config/mse/<name>` |
| Data: app state, media, downloads | `~/.local/share/mse/<name>` |
| Generated files and logs | `~/.local/state/mse/<name>` |
| The secrets key | `~/.config/sops/age/keys.txt` |

## Running unattended

| When | What | Where |
|---|---|---|
| daily at 04:30 | back up | the main |
| daily at 05:00 | update and apply | every machine |
| Sundays at 05:30 | check the backups | the main |
| every 15 minutes | remove executable downloads | every machine |

`systemctl --user list-timers` lists them. They need lingering, which `mse check-machine` checks.

## More

- [Commands](docs/COMMANDS.md): what each command does, in detail.
- [Backups](docs/BACKUP.md), [Restoring](docs/RESTORE.md) and [Upgrading](docs/UPGRADING.md).
- Each config repository has a README and a CONFIG.md describing its files.

## Developing

`make test` runs the Go and bats tests, shellcheck and golangci-lint; CI runs them on every pull request. [CONTRIBUTING.md](CONTRIBUTING.md) has the rest.
