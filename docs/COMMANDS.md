# mse commands

`mse help` lists every command; `mse <command> --help` explains one. This page is the longer version.

- [Installing mse](#installing-mse)
- [Where things live](#where-things-live)
- [mse check-machine](#mse-check-machine)
- [mse setup](#mse-setup)
- [mse configure](#mse-configure)
- [mse update](#mse-update)
- [mse apply](#mse-apply)
- [mse status](#mse-status)
- [The timers](#the-timers)
- [Backups](#backups)
- [The stack and the landing page](#the-stack-and-the-landing-page)
- [Output and logs](#output-and-logs)

## Installing mse

`mse` is a single Go binary, built for Linux and macOS on amd64 and arm64 and installed from a GitHub release:

    curl -fsSL https://github.com/pablovarela/media-server-engine/releases/latest/download/install.sh | sh

- Installing and updating `mse` need no GitHub login: the releases are public. gh is needed for the config repository.
- It installs the latest release into `~/.local/bin`. On its side of the pipe, `MSE_VERSION=v0.7.0` picks a release and `MSE_INSTALL_DIR` another folder: `… | MSE_VERSION=v0.7.0 sh`.
- It checks the archive against the release's `checksums.txt`, and leaves the installed `mse` in place when the download or the check fails.
- From a clone, `make go-build` builds `dist/mse` for the machine it runs on.

## Where things live

`mse` keeps an installation in the XDG base directories:

| What | Where | Moved by |
|---|---|---|
| Config (a clone of `media-server-config-<name>`) | `~/.config/mse/<name>` | `XDG_CONFIG_HOME` |
| Data: app state, media, downloads | `~/.local/share/mse/<name>` | `XDG_DATA_HOME` |
| Generated files, decrypted secrets included | `~/.local/state/mse/<name>` | `XDG_STATE_HOME` |
| Logs | `~/.local/state/mse/<name>/logs/mse.log` | `XDG_STATE_HOME` |
| The pinned restic | `~/.cache/mse/restic` | `XDG_CACHE_HOME` |
| The secrets key | `~/.config/sops/age/keys.txt` | `SOPS_AGE_KEY_FILE` |

- Media and downloads share one folder, `data/data/`, mounted as `/data` in Deluge, Radarr, Sonarr, Bazarr and Jellyfin: downloads in `/data/downloads`, films in `/data/media/movies`, series in `/data/media/tvshows`. One mount lets Radarr and Sonarr import a finished download as a hardlink, so it is neither written again nor stored twice; Linux refuses a hardlink across two mounts, even on one disk.
- A machine runs one installation, and `mse` uses it. When it finds several under `~/.config/mse`, it refuses and lists them.
- The age key can also come from `SOPS_AGE_KEY` or `SOPS_AGE_KEY_CMD`.
- `mse` talks to the Docker daemon directly: no `sops` or `docker compose` command is needed.

## mse check-machine

Checks the machine has what an installation needs. It changes nothing.

In order:
1. git;
2. gh logged in, with its token on disk (the nightly update fetches the config with it; a `GITHUB_TOKEN` in the shell isn't enough);
3. git using gh for github.com (`gh auth setup-git`), which is how the config is fetched and pushed;
4. Docker, the user in the `docker` group, and Docker answering this session;
5. lingering, and a user manager that has the docker group (under systemd);
6. the stack's ports, free or held by the stack itself.

Each line is ✓, ✗ with the command that fixes it, ? when it couldn't be checked, or – when it waits for an earlier one. Run it again until it says `This machine is ready.`; it exits 1 until then.

## mse setup

`mse setup <name>` sets up this machine for the installation `<name>`. It looks for its config repository, `media-server-config-<name>` under the user gh is logged in as (`--owner <org>` for an organisation), and takes one of two paths. It runs the `check-machine` checks first on both.

It refuses, before writing anything, when the name isn't valid (lowercase letters, digits and `-`, starting with a letter), when this machine already has `<name>` (`mse status` shows its state), or when it runs another installation (one installation per machine).

### When the repository doesn't exist: a new installation

It asks first: "There's no media-server-config-<name> under <owner>. Create a new installation called <name>?" No answer, or no, creates nothing. Then:

1. **Secrets key:** a new age key, added to `~/.config/sops/age/keys.txt` (or `SOPS_AGE_KEY_FILE`; it refuses while `SOPS_AGE_KEY` or `SOPS_AGE_KEY_CMD` is set). It is shown once, on the terminal only: save it. The name and the key rebuild the installation anywhere.
2. **Config:** written from the template into `~/.config/mse/<name>`, then every configure section is asked in turn.
3. **Repository:** a commit, the new private repository and a push.
4. **Main:** it creates the backup repository and backs up the still-empty data folder, as `mse backup --take-over` does, so the repository records this machine as the main.
5. **Apply:** `mse apply`.

- `--homepage-port <port>` puts the landing page on another port, for the checks and in the settings, when something else on the machine uses port 80.
- If it fails, or you quit, before the repository exists, it removes what it wrote, the key included. After that, it says which commands finish the job.

### When the repository exists: a rebuild

It says "Rebuilding <name> from <owner>/media-server-config-<name>." Then:

1. **Clone:** the config into `~/.config/mse/<name>`.
2. **Secrets key:** if no key on the machine decrypts the config's secrets, it asks for the key. It accepts only a key that opens every secret file, and adds it to `~/.config/sops/age/keys.txt`.
3. **Data:**
   - an empty data folder gets the latest backup;
   - app data already there is kept (a disk moved from the old machine); `--overwrite` moves it aside and restores the latest backup;
   - with no backups yet, the apps start empty.
4. **Main:**
   - when this machine made the latest backup, it is the main;
   - otherwise setup asks whether this machine becomes the main: "yes" by default when there are no backups yet or the machine behind the latest one looks gone, "no" while that machine backed up in the last two days. A machine that becomes the main backs up straight away;
   - a machine that kept its own app data while the main backed up in the last two days doesn't back up, so that data can't become the latest backup.

   A machine that doesn't take over doesn't back up; `mse backup --take-over` changes that later.
5. **Apply:** `mse apply`.

- A backup holds app state, not media: copy the media over, or rescan each app.
- The landing page's port belongs to the installation: `--homepage-port` is refused here. If something else on the machine needs port 80, change it with `mse configure` from a machine that has the installation.
- If it fails, or you quit, before the restore, it removes the clone and any key it added. After that, it says which commands finish the job.

## mse configure

Changes the installation's settings and secrets from a menu in the terminal, then commits and pushes them.

- **Sections:** General (time zone, Jellyfin admin user, homepage port and host names), Backups, VPN, Healthchecks, App logins, and new random internal API keys for Sonarr, Radarr and Prowlarr.
- It updates the config first, and refuses while the config has uncommitted changes.
- Secret fields are masked: Enter keeps the current value, and `-` removes an optional one (an OpenVPN login for WireGuard, a Healthchecks key, a B2 key).
- Saving shows what changed (secrets only as "changed"), writes `installation.env`, re-encrypts only the secret files that changed, commits and pushes.
- Lines it doesn't manage, comments included, stay as they are.
- Nothing is applied on the machine it runs on: run `mse apply`, or wait for the nightly update.
- It needs a terminal, and a git name and email for the config.

## mse update

- Fast-forwards the config from its remote. It refuses while the config has uncommitted changes.
- Replaces the installed `mse` with the newest release of its major version, after checking it against `checksums.txt` and running it once.
- A newer major version can need config changes, so `mse update` only says it is available; `mse update --force` installs it.
- See [Upgrading](UPGRADING.md) for images, major versions and going back.
- `mse update --apply` waits for a running backup, updates, then applies with the updated `mse`, and reports to healthchecks.io. The nightly timer runs it.

## mse apply

Applies the config on disk to the machine, uncommitted changes included:

1. writes the secrets and draws the landing page;
2. sets up the healthchecks.io checks (under systemd, with `HEALTHCHECKS_MANAGE_KEY`);
3. pulls the stack's images, trying again when a registry limits requests; a failed pull stops it before anything restarts;
4. starts the stack, and puts Prowlarr, FlareSolverr and Deluge back on gluetun's network if they lost it;
5. wires the apps as the config's `apps.yml` and `prowlarr.yml` declare: Prowlarr, Jellyfin, Sonarr's and Radarr's library updates, Deluge, Configarr, Seerr, Bazarr, Maintainerr;
6. reloads Homepage and removes outdated images;
7. sets up the timers (below), even when an earlier step failed, so the nightly update is there to try again.

Each change the wiring makes is one line. An app that fails or doesn't answer within five minutes is named, the others are still wired, and the command fails.

## mse status

Shows this machine's installation. It changes nothing and prints no passwords.

- **What it shows:**
  - the installation and the `mse` version;
  - whether this machine is the main, and when the latest backup was made and by which machine;
  - how many of the stack's containers are running;
  - each timer's last run, whether it succeeded, and its next run;
  - the address of the landing page and of every app.
- **When a part can't be read,** for example the backup repository is unreachable, that part says why and the rest still shows. Without systemd there are no timers to show.
- **Exit code:** 1 when something needs attention, listed at the end:
  - a timer's last run failed;
  - a container of the stack isn't running, or the stack has no containers;
  - the backup repository, the stack or the timers couldn't be read;
  - this machine is the main and its latest backup is more than 2 days old.

  Otherwise 0.

## The timers

| Timer | Runs | When | Where |
|---|---|---|---|
| `mse-<name>-update` | `mse update --apply` | daily at 05:00 | every machine |
| `mse-<name>-download-cleanup` | `mse clean-downloads` | every 15 minutes | every machine |
| `mse-<name>-backup` | `mse backup` | daily at 04:30 | the main |
| `mse-<name>-verify` | `mse check-backup` | Sundays at 05:30 | the main |

- They are systemd user units in `~/.config/systemd/user`: `systemctl --user list-timers` lists them, and `journalctl --user-unit mse-<name>-update.service` shows a run.
- They run with nobody logged in once lingering is on: `sudo loginctl enable-linger <user>`, once. Until then `mse apply` says so and leaves them out.
- Add the user to the `docker` group before turning lingering on: the user's systemd keeps the groups it started with until it restarts. `mse apply` warns when it started without `docker`; `sudo systemctl restart user@<uid>`, or a reboot, fixes it.
- Which machine is the main comes from the backup repository; any other machine has no backup timers.
- The units call the installed `mse` by its full path and carry `XDG_*_HOME`, `SOPS_AGE_KEY_FILE` and `SOPS_AGE_KEY_CMD` as the shell running `mse apply` has them. A dev build sets up no timers.
- The nightly update fetches the config from GitHub with nobody logged in, so `gh auth login` must have kept its token in `~/.config/gh/hosts.yml` (where it goes without a keyring); `mse apply` warns when it hasn't.

## Backups

- **Commands:** `mse backup [--take-over [--yes]]`, `mse check-backup`, `mse restore`.
- **Repository:** `RESTIC_REPOSITORY` in the config's `installation.env`, with its credentials in `secrets/backup.sops.env`.
- **The main:** only the installation's main backs up: the machine that made the latest snapshot. `mse status` says which. On any other machine `mse backup` and `mse check-backup` say so and exit without failing; `mse backup --take-over` makes this machine the main, after asking when another machine is, and backs up (`--yes` skips the question).
- **Healthchecks:** `mse backup` and `mse check-backup` report to healthchecks.io themselves when the config has a ping key, so a run by hand counts like a timer run.
- **Paths:** a backup runs restic from the data folder with symlinks resolved, so a data folder linked to another path backs up under the same paths as before.
- **More:** [Backups](BACKUP.md) and [Restoring](RESTORE.md).
- **restic:** `mse` runs the restic version pinned in it. It downloads it from restic's GitHub releases the first time it needs it, checks it against the checksum built into `mse`, and keeps it in `~/.cache/mse/restic`. Before each use it checks that copy against the checksum it recorded, and downloads it again if it changed. A restic on `PATH` isn't used.

## The stack and the landing page

- `mse stack up|down|restart|ps|logs` run the installation's containers.
- `mse stack up --wait` returns once every container is running, and healthy when it has a healthcheck (`--wait-timeout`, 5 minutes by default).
- `mse stack logs -f [service...]` follows the logs; `--tail N` limits them to the last lines.
- When the config pins a `homepage` image, `mse stack up` and `mse stack restart` draw the landing page first, from the config's `homepage/` files over the engine's default page, then reload Homepage. A page that can't be drawn is reported and the containers start anyway. `mse stack restart homepage` redraws it on its own.
- `mse logins` prints the apps' users and passwords; `mse status` lists their addresses.

## Output and logs

- Commands say what they are doing, one step per line. `--verbose` (`-v`) also shows the output of restic and Compose.
- On a terminal `mse` colours its output; `NO_COLOR` turns that off.
- Every run is logged in full, whatever the flags, to `~/.local/state/mse/<name>/logs/mse.log` (`~/.local/state/mse/mse.log` before an installation is known). It is rotated at 10 MB with five compressed files kept.
- Each line carries the command and a run id, so `grep 'backup\[3f9a2c\]'` gives one run.
