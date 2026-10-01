# Restoring

## Rebuilding a machine

To rebuild an installation on a new machine, after losing or replacing the old one, join it:

```
make join-installation NAME=<name>
```

It restores the latest backup before the apps start, then wires them, so they come back with their libraries, history, users and connections. Answer yes when asked whether it should be the main if the old main is gone. See [Installing](INSTALL.md) for the steps.

## Restoring in place

To roll a machine's app state back to the latest backup:

```
cd ~/<name>
make media-stop
make restore ARGS=--overwrite
make update
```

`make restore` refuses while the apps run. With `--overwrite`, the current `data/volumes/` is moved aside to `data/volumes.before-restore-<time>` rather than deleted; remove it once the restored apps look right. Without it, restore only writes into an empty `data/volumes/`. `make update` brings the apps up and wires them again.

## What comes back, and what does not

A backup holds app state, not media. After restoring onto a machine without the media, the apps list the titles the old machine had. A library scan in Jellyfin and a rescan in Sonarr and Radarr bring them in line with what is on the disk.

Restores take the latest snapshot of the installation, and the latest snapshot of any host while the installation has no snapshot of its own yet.
