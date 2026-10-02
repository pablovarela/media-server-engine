# Installing

## What you need

- A Raspberry Pi 4 or 5 with Raspberry Pi OS 64-bit, or another arm64 Debian machine, for an installation that runs all the time. A Mac with Docker (OrbStack or Docker Desktop) works for trying it out.
- An account with a VPN provider that gluetun supports, with its OpenVPN username and password.
- Optional: a GitHub account and the GitHub CLI (`gh auth login`), to keep the config on GitHub so other machines can join.
- Optional: a Backblaze B2 bucket and application key for off-site backups; a local folder works too.
- Optional: a healthchecks.io project and its ping key, to be told when a backup or update fails.

`make bootstrap` installs the rest: sops, age, restic and, on Debian, Docker. The create and join targets run it for you. On a fresh Debian machine it adds you to the `docker` group, which only applies after logging in again: if create or join stops saying docker cannot be used yet, log out and back in, then run `make setup-machine` from `~/<name>` to finish.

## Creating an installation

```
cd media-server-engine
make create-installation NAME=<name>
```

The name is lowercase letters, digits and dashes. It names the installation's folder, its config repository, its backups and its healthchecks, and it cannot be changed later.

1. The engine copies itself into `~/<name>/engine` (set `INSTALL_DIR` to use another folder) and continues from there; config and data go next to it.
2. A secrets key is made and shown once. Save the `AGE-SECRET-KEY-...` line in your password manager before pressing Enter: without it, nothing in the config can be decrypted on another machine.
3. `make configure` checks that git has a name and email to commit the config with, then asks for the settings: time zone, where to keep the config (only on this machine, or a private GitHub repository), where to keep the backups (a local folder or Backblaze B2, checked before it is accepted, or another restic repository such as sftp or s3), the VPN, the healthchecks ping key and, for the landing page, an optional read-only healthchecks API key, the app passwords and the subtitle languages. Pressing Enter on a password generates one; `make logins` shows it later. The landing page asks only for its port: 80 when it is free on this machine, otherwise the first free one from 8080 is suggested. Everything else on it can be changed later in the config's `homepage/` files.
4. You are asked whether this machine is the installation's main, the one that backs up. Say yes on the first machine.
5. `make update` pulls the images, brings the apps up and wires them. On the main, the first backup runs and creates the backup repository.
6. On machines with systemd, timers are installed for the daily update, the fake-download cleanup and, on the main, the backups.
7. A summary shows where the installation lives, the app addresses and the everyday commands, and ends with the `cd` into the installation, ready to copy (make cannot change the directory of the shell it runs from).

If create stops before the settings are saved, it removes what it made, including the new key (delete it from your password manager too), so it can simply be run again. If only publishing the config to GitHub fails, the settings are kept on this machine and the next `make configure` publishes them. If it stops later, the settings are kept and `make setup-machine` from `~/<name>` finishes the job.

Jellyfin shows new titles about a minute after Sonarr or Radarr imports them, except the very first title of a new installation, which appears at the next library scan or with Scan All Libraries in Jellyfin's dashboard.

## Adding a machine, or rebuilding one

For an installation whose config is on GitHub:

```
cd media-server-engine
make join-installation NAME=<name>
```

1. The engine copies itself into `~/<name>/engine`, as when creating.
2. A deploy key is made for the engine repository, read-only, and one for the config repository, with write access so that `make configure` on this machine can push what it changes. With the GitHub CLI logged in they are added for you; otherwise each key and the page to add it are shown (tick "Allow write access" for the config's), and join waits until GitHub accepts them.
3. The config is cloned, and you are asked for the secrets key if this machine does not have it yet.
4. The latest backup is restored, so the apps come back with their libraries, history and users. Set `SKIP_RESTORE=1` when the data is already in place.
5. You are asked whether this machine should be the main. Say no while another machine backs up; say yes to take over from a machine that is gone.
6. The update, timers and summary follow, as when creating.

A backup holds app state, not media. A machine rebuilt without the media shows the titles the old machine had; a rescan in each app brings them in line with what is on its disk.

## Running it

Everything runs by hand with make from the installation, `~/<name>`, on any machine. Its Makefile passes every target to the engine's, so `make` there works the same as in `~/<name>/engine`, and `make` alone lists the everyday targets first. The systemd timers are one supported way to schedule the same commands:

| Timer | When | Runs |
|---|---|---|
| `media-update` | daily 05:00 | `make update`, pinging healthchecks |
| `media-download-cleanup` | every 15 minutes | removes downloads Sonarr or Radarr flag as executables |
| `media-backup` | daily 04:30, main only | `make backup-now` |
| `media-verify` | Sundays 05:30, main only | `make verify-backup-now` |

A timer whose time passed while the machine was off runs once when it is back. `make install-update-timer`, `make install-download-cleanup-timer` and `make install-backup-timers` install them by hand.
