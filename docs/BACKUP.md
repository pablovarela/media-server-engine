# Backups

## What is backed up

- **Backed up:** `volumes/` in the data folder: every app's configuration and state (databases, settings, users, watch history, Deluge's torrent list and plugins). Caches, logs, Jellyfin's downloaded metadata and Configarr's checkouts are left out.
- **Not backed up:** media and downloads. They are large and can be fetched again.
- **Not in the backup:** the config. It lives in its git repository, with its secrets encrypted with the installation's secrets key.

## Where

| Setting | In |
|---|---|
| `RESTIC_REPOSITORY`: a folder, `b2:<bucket>:<folder>`, or any repository restic supports (`sftp:`, `s3:`…) | `installation.env` |
| `RESTIC_PASSWORD`, `B2_ACCOUNT_ID`, `B2_ACCOUNT_KEY`, and any other variable restic needs for the repository | `secrets/backup.sops.env` |

`mse configure` sets them (Backups section). Keep the secrets key in a password manager: without it the restic password, and so the backups, can't be read.

restic keeps 7 daily, 4 weekly and 6 monthly snapshots, and removes older ones after each backup.

## Which machine backs up

- One machine per installation backs up: its **main**, the machine that made the latest snapshot. Snapshots are tagged with the machine's id and name.
- Any other machine refuses to back up, so two machines never write competing snapshots. The refusal names the main and when it last backed up, and isn't reported as a failure; checking the backups is refused the same way, since the main does that too.
- `mse status` says which machine is the main. `mse backup --take-over` makes this machine the main: it backs up once, and from then on the old main refuses. When another machine is the main, it first names it and the time of its last backup, and asks; `--yes` takes over without asking.
- `mse setup` makes a new installation's machine the main; when it rebuilds an installation, it asks.

## Running a backup

- `mse backup` stops the apps, takes the snapshot, starts them again and removes old snapshots. It takes a few minutes. A lock stops a second backup while one runs; the operating system releases it however the backup ends.
- `mse check-backup` runs `restic check`, restores the latest snapshot's databases into a temporary folder and checks each one.
- On machines with systemd, the main's timers back up daily at 04:30 and verify on Sundays at 05:30.
- `mse` runs the restic version pinned in it, downloaded on first use into `~/.cache/mse/restic`; nothing needs installing.

## Locks in the backup repository

- A restic process that is killed (a power cut, a sleeping laptop, an interrupted command) leaves its lock behind.
- A backup, check or restore first removes stale locks: those whose process is gone from this machine, and those nobody refreshed for 30 minutes.
- If a lock still gets in the way, restic waits up to two hours, then the job fails and names the machine, process and time behind each lock. A lock whose process is gone goes stale 30 minutes after its last refresh, and the next run removes it.

## Being told when something fails

With a healthchecks.io ping key in `secrets/healthchecks.sops.env`, backups, checks and nightly updates ping their check:

| Check | Pinged by | Schedule | Grace |
|---|---|---|---|
| `<name>-backup` | every backup, on the main | `30 4 * * *` | 2 hours |
| `<name>-verify` | every backup check, on the main | `30 5 * * 0` | 4 hours |
| `<name>-update` | the nightly update on the main | `0 5 * * *` | 2 hours |
| `<name>-update-<host>` | the nightly update on other machines | `0 5 * * *` | 2 hours |

With a read-write API key (`HEALTHCHECKS_MANAGE_KEY`), `mse apply` on a machine with systemd sets up the checks this machine pings: schedule (in the timers' time zone), grace, the project's integrations, a tag and a description of what to do when it fails. Without it, checks are created by their first ping with healthchecks.io's defaults.
