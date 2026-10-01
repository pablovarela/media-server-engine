# Backups

## What is backed up

`data/volumes/`: every app's configuration and state (databases, settings, users, watch history, Deluge's torrent list and plugins), with caches, logs, Jellyfin's downloaded metadata and Configarr's checkouts left out. Media and downloads are not backed up: they are large and can be fetched again.

The config repository is not part of the backup. It lives in git, locally or on GitHub, and its secrets are encrypted with the installation's secrets key.

## Where

In a restic repository, set by `make configure`: a local folder (`RESTIC_REPOSITORY=/path/to/folder`) or a Backblaze B2 bucket (`RESTIC_REPOSITORY=b2:<bucket>:<folder>`). The restic password and the B2 key are in `secrets/backup.sops.env`. Keep the installation's secrets key in your password manager: it decrypts the restic password, and without it the backups cannot be read.

restic keeps the last 7 daily, 4 weekly and 6 monthly snapshots, and removes older ones after each backup.

## Which machine backs up

One machine per installation backs up: its main. Snapshots are tagged with the machine that made them, and the machine behind the latest snapshot is the main. Any other machine refuses to back up, so two machines never write competing snapshots.

- The first machine of a new installation becomes the main with its first backup; the backup repository is created then.
- `make claim-backup-main` makes this machine the main: it runs one backup tagged with it, and from then on the old main refuses. Use it when the main is replaced.

## Running a backup

`make backup-now` stops the apps, takes the snapshot, starts them again and prunes old snapshots. It takes a few minutes. A lock stops a second backup while one runs; a lock left by a backup that died is taken over.

`make verify-backup-now` runs `restic check`, restores the latest snapshot into a temporary folder and checks every SQLite database in it with `PRAGMA integrity_check`.

On machines with systemd, the main backs up daily at 04:30 and verifies on Sundays at 05:30.

## Being told when something fails

With a healthchecks.io ping key in `secrets/healthchecks.sops.env`, backups, checks and scheduled updates ping `https://hc-ping.com/<ping key>/<check>`, and the checks are created by their first ping:

| Check | Pinged by |
|---|---|
| `<name>-backup` | every backup, on the main |
| `<name>-verify` | every backup check, on the main |
| `<name>-update` | the scheduled update on the main |
| `<name>-update-<host>` | the scheduled update on other machines |

New checks get healthchecks.io's default schedule (daily, one hour of grace); set `<name>-verify` to weekly in healthchecks.io.
