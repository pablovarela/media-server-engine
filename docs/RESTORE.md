# Restoring

## Rebuilding a machine

After losing or replacing the machine, on the new one:

    mse check-machine
    mse setup <name>

`mse setup` finds the config repository, so it rebuilds rather than creates. It restores the latest backup before the apps start (into an empty data folder; app data already there is kept unless `--overwrite` is given), then wires them, so they come back with their libraries, history, users and connections. When the old main is gone, it offers to make this machine the main. See [Commands](COMMANDS.md#mse-setup).

## Restoring in place

To roll this machine's app state back to the latest backup:

    mse stack down
    mse restore --overwrite
    mse apply

- `mse restore` refuses while the apps run.
- With `--overwrite`, the current `volumes/` is moved aside to `volumes.before-restore-<time>` rather than deleted; remove it once the restored apps look right. Without it, restore only writes into an empty `volumes/`.
- `mse apply` brings the apps up and wires them again.

## What comes back, and what doesn't

- **Back:** app state: libraries, watch history, users, settings, the download client's torrent list.
- **Not back:** media and downloads. On a machine without the media, the apps list the titles the old machine had; a library scan in Jellyfin and a rescan in Sonarr and Radarr bring them in line with the disk.
- A restore takes the installation's latest snapshot.
