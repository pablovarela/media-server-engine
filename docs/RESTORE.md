# Restoring

## Rebuilding a machine

After losing or replacing the machine, on the new one:

    mse check-machine
    mse setup <name>

`mse setup` finds the config repository, so it rebuilds rather than creates. It restores the latest backup before the apps start (into an empty data folder; app data already there is kept unless `--overwrite` is given), then wires them, so they come back with their libraries, history, users and connections. When the old main is gone, it offers to make this machine the main. See [Commands](COMMANDS.md#mse-setup).

### With a media backup

When the installation has a media backup and `mse setup` restored the apps, it leaves the stack stopped and ends with:

    <name> is set up without its media. Bring it back, then start, with:
      mse restore --media
    Or start without it:
      mse apply

The stack stays stopped so that Sonarr, Radarr and Jellyfin first see the whole library. Started without it, they mark the titles missing, search for them and download them again, and Jellyfin drops them from its library.

When the media lived on another disk, mount that disk and make `data/media` a link to its media folder before restoring; otherwise the media is restored onto the system disk. `mse restore --media` names the folder it restores into and the free space there.

`mse restore --media`:

- refuses while the apps run;
- restores the latest media snapshot into `data/media` (a link to another disk is followed), keeping the files already there that match, so running it again after an interruption carries on where it stopped;
- then starts the stack with `mse apply`.

## Restoring in place

To roll this machine's app state back to the latest backup:

    mse stack down
    mse restore --apps --overwrite
    mse apply

- `mse restore --apps` refuses while the apps run.
- With `--overwrite`, the current `volumes/` is moved aside to `volumes.before-restore-<time>` rather than deleted; remove it once the restored apps look right. Without it, restore only writes into an empty `volumes/`.
- `mse apply` brings the apps up and wires them again.

## What comes back, and what doesn't

- **Back:** app state: libraries, watch history, users, settings, the download client's torrent list.
- **Back with a media backup:** the media, with `mse restore --media`.
- **Not back:** downloads, and media without a media backup. On a machine without the media, the apps list the titles the old machine had; a library scan in Jellyfin and a rescan in Sonarr and Radarr bring them in line with the disk.
- A restore takes the installation's latest snapshot.
