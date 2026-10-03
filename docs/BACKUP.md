# Backups

## What is backed up

`data/volumes/`: every app's configuration and state (databases, settings, users, watch history, Deluge's torrent list and plugins), with caches, logs, Jellyfin's downloaded metadata and Configarr's checkouts left out. Media and downloads are not backed up: they are large and can be fetched again.

The config repository is not part of the backup. It lives in git, locally or on GitHub, and its secrets are encrypted with the installation's secrets key.

## Where

In a restic repository, set by `make configure`: a local folder (`RESTIC_REPOSITORY=/path/to/folder`), a Backblaze B2 bucket (`RESTIC_REPOSITORY=b2:<bucket>:<folder>`), or another kind of repository restic supports, such as `sftp:` or `s3:`. For another kind, add the variables restic needs for it (for example `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`) to `secrets/backup.sops.env` with `sops edit`; configure keeps them, but does not check that repository. The restic password and the B2 key are in `secrets/backup.sops.env`. Keep the installation's secrets key in your password manager: it decrypts the restic password, and without it the backups cannot be read.

restic keeps the last 7 daily, 4 weekly and 6 monthly snapshots, and removes older ones after each backup.

## Which machine backs up

One machine per installation backs up: its main. Snapshots are tagged with the machine that made them (its id and its name), and the machine behind the latest snapshot is the main. Any other machine refuses to back up, so two machines never write competing snapshots.

- The first machine of a new installation becomes the main with its first backup; the backup repository is created then.
- `make claim-backup-main` makes this machine the main: it runs one backup tagged with it, and from then on the old main refuses. When another machine is the main, it first names that machine and the time of its last backup, and asks. Use it when the main is replaced: a claim made while the old main still runs also replaces that day's snapshot from the old main.

## Running a backup

`make backup-now` stops the apps, takes the snapshot, starts them again and prunes old snapshots. It takes a few minutes. A lock stops a second backup while one runs. The operating system holds it for the backup and the restic it started, and releases it once they have ended, however they end, so a backup that died or a reboot never leaves it behind.

`make verify-backup-now` runs `restic check`, restores the latest snapshot into a temporary folder and checks every SQLite database in it with `PRAGMA integrity_check`.

On machines with systemd, the main backs up daily at 04:30 and verifies on Sundays at 05:30.

## Locks in the backup repository

restic locks the repository while it works, and a restic process that is killed (a power cut, a laptop going to sleep, a command interrupted) leaves its lock behind. A backup, check or restore first removes stale locks: those whose process is gone from this machine, and those nobody has refreshed for 30 minutes. A running restic refreshes its lock every few minutes, so it keeps it. If a lock still gets in the way, restic waits up to two hours, then the job fails and names the machine, process and time behind each lock.

`make unlock-backup` removes stale locks and lists the ones left. `make unlock-backup ALL=1` removes every lock; use it only when no machine is running restic on the repository.

## Being told when something fails

With a healthchecks.io ping key in `secrets/healthchecks.sops.env`, backups, checks and scheduled updates ping `https://hc-ping.com/<ping key>/<check>`, and the checks are created by their first ping:

| Check | Pinged by |
|---|---|
| `<name>-backup` | every backup, on the main |
| `<name>-verify` | every backup check, on the main |
| `<name>-update` | the scheduled update on the main |
| `<name>-update-<host>` | the scheduled update on other machines |

With a read-write API key of the same project as `HEALTHCHECKS_MANAGE_KEY`, every `make update` also sets up the checks this machine pings: the main its backup, verify and update checks, any other machine its own update check. Each gets its schedule in the time zone this machine's timers run in, its grace, all the project's integrations, its job's name as a tag and a description saying what runs and what to do when it fails; a description or integration changed in healthchecks.io is replaced.

| Check | Schedule | Grace |
|---|---|---|
| `<name>-backup` | `30 4 * * *` | 2 hours |
| `<name>-verify` | `30 5 * * 0` | 4 hours |
| `<name>-update`, `<name>-update-<host>` | `0 5 * * *` | 2 hours |

Without that key, checks get healthchecks.io's default schedule (daily, one hour of grace) and are set up by hand.
