# Backups

## What is backed up

- **Backed up:** `volumes/` in the data folder: every app's configuration and state (databases, settings, users, watch history, Deluge's torrent list and plugins). Caches, logs, Jellyfin's downloaded metadata and Configarr's checkouts are left out.
- **Not backed up by default:** media and downloads. The [media backup](#media-backup) adds the media, as a separate, weekly backup; downloads are never backed up.
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
- `mse status` says which machine is the main. `mse backup --apps --take-over` makes this machine the main: it backs up once, and from then on the old main refuses. When another machine is the main, it first names it and the time of its last backup, and asks; `--yes` takes over without asking.
- `mse setup` makes a new installation's machine the main; when it rebuilds an installation, it asks.

## Running a backup

- `mse backup --apps` stops the apps, takes the snapshot, starts them again and removes old snapshots. It takes a few minutes. A lock stops a second backup while one runs; the operating system releases it however the backup ends.
- `mse check-backup` runs `restic check`, restores the latest snapshot's databases into a temporary folder and checks each one.
- `mse backup --media` backs up the media; `mse backup --apps --media` does both, apps first.
- On machines with systemd, the main's timers back up daily at 04:30 and verify on Sundays at 05:30, and back up the media when it is on.
- `mse` runs the restic version pinned in it, downloaded on first use into `~/.cache/mse/restic`; nothing needs installing.

## Locks in the backup repository

- A restic process that is killed (a power cut, a sleeping laptop, an interrupted command) leaves its lock behind.
- A backup, check or restore first removes stale locks: those whose process is gone from this machine, and those nobody refreshed for 30 minutes.
- If a lock still gets in the way, restic waits up to two hours, then the job fails and names the machine, process and time behind each lock. A lock whose process is gone goes stale 30 minutes after its last refresh, and the next run removes it.

## Media backup

An optional second backup holds `data/media`, the films and episodes, so a rebuilt machine gets its whole library back. It is off by default.

- **Separate:** it has its own restic repository and its own weekly timer. The nightly apps backup stays small and fast, and pruning or checking the apps backup never reads the media.
- **The stack keeps running:** media files are written once, when they are imported, so a snapshot of a running library is consistent. It runs at low CPU and I/O priority.
- **Only the main:** the media backup runs on the apps backup's main, and refuses on any other machine the same way.
- **Each run checks itself:** after the snapshot it removes the snapshots past the weeks kept, then reads back a sample of the repository (`restic check --read-data-subset`).
- **Not backed up:** `data/downloads`, which holds seeding torrents and partial downloads; anything imported is already in the media.

### Settings

`mse configure`, Media backup section. All of them are in `installation.env`; empty means the engine's default.

| Setting | Means | Empty means |
|---|---|---|
| `MEDIA_BACKUP` | `yes` to turn it on | off |
| `MEDIA_RESTIC_REPOSITORY` | the media's restic repository | the backup repository with `-media` after it |
| `MEDIA_BACKUP_KEEP_WEEKLY` | weekly snapshots kept | 4 |
| `MEDIA_BACKUP_UPLOAD_LIMIT` | upload cap in KiB/s | no cap |
| `MEDIA_BACKUP_SCHEDULE` | weekday and time it runs, like `Sun 01:00` | `Sun 01:00` |
| `MEDIA_BACKUP_CHECK_SUBSET` | share of the repository each run reads back | `5%` |

- The media repository uses the backup repository's password and keys (`RESTIC_PASSWORD`, `B2_ACCOUNT_ID`, `B2_ACCOUNT_KEY`).
- The default repository sits beside the backup repository, in the same bucket or folder: `b2:<bucket>:<folder>` gives `b2:<bucket>:<folder>-media`. A repository at a bucket's root, or on another kind of storage, needs `MEDIA_RESTIC_REPOSITORY` set.
- On B2, the key must cover the whole bucket. A key limited to a file-name prefix can't reach the media repository; the backup says so when B2 refuses it.
- On B2, set the bucket's lifecycle rule to keep only the last version of each file, or pruned data keeps costing.

### Size

The repository holds the library plus what changed in the weeks kept: roughly the media's size, plus the weekly churn times the weeks kept. For example, 100 GB of media that changes by 10 GB a week, kept 4 weeks, needs about 140 GB.

### The first run

Run `mse backup --media` once by hand after turning it on: the first upload sends the whole library and can take hours or days, longer with an upload cap. Later runs send only what changed. The media backup tells healthchecks.io only how it ended, not when it started, so a long upload isn't reported as stuck; if it is still running when the next weekly run is due, that run says a media backup is already running.

## Being told when something fails

With a healthchecks.io ping key in `secrets/healthchecks.sops.env`, backups, checks and nightly updates ping their check:

| Check | Pinged by | Schedule | Grace |
|---|---|---|---|
| `<name>-backup` | every backup, on the main | `30 4 * * *` | 2 hours |
| `<name>-verify` | every backup check, on the main | `30 5 * * 0` | 4 hours |
| `<name>-media-backup` | every media backup, on the main | from `MEDIA_BACKUP_SCHEDULE` | 24 hours |
| `<name>-update` | the nightly update on the main | `0 5 * * *` | 2 hours |
| `<name>-update-<host>` | the nightly update on other machines | `0 5 * * *` | 2 hours |

With a read-write API key (`HEALTHCHECKS_MANAGE_KEY`), `mse apply` on a machine with systemd sets up the checks this machine pings: schedule (in the timers' time zone), grace, the project's integrations, a tag and a description of what to do when it fails. On the main, it deletes `<name>-media-backup` while the media backup is off, so turning it off doesn't leave a check that goes down a week later. Without it, checks are created by their first ping with healthchecks.io's defaults.
