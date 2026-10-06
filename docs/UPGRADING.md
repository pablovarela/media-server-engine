# Upgrading

An installation runs the `mse` release installed on each machine, and exactly the images its config pins (tag and digest, in `images.yml` and `images.monitoring.yml`).

## Images

- Add the config repository to the Renovate app. It opens a pull request for each new image and keeps a Dependency Dashboard issue listing them.
- Merging the pull request is the upgrade: every machine with systemd applies it at its nightly update (05:00), or straight away with `mse update --apply`.
- By hand: change an image in the config (always with a digest), commit, push, and run `mse update --apply`.

## mse

- `mse update` installs the newest release of the running major version, after checking it against the release's `checksums.txt`. The nightly update runs it.
- A new major version can need config changes: `mse update` says it is available, and `mse update --force` installs it. The config's `config:` number in `config.yml` is the major version it is written for; `mse` refuses a config of another schema.
- Read a release's notes on GitHub before installing a new major: they say what a config needs.

## Going back

- **An image:** revert the config commit that changed it, push, and run `mse update --apply`.
- **mse:** install an earlier release with `install.sh` and `MSE_VERSION=<version>`. It runs until the next nightly update installs the newest release of its major again. An earlier major also needs a config written for it (its `config:` number).
- App data is not migrated back: if an app upgraded its database, restore the backup taken before the upgrade (see [Restoring](RESTORE.md)).

## What the nightly update does

`mse update --apply`, from the timers at 05:00:

1. waits for a running backup;
2. fast-forwards the config from its remote, refusing while it has uncommitted changes;
3. installs a newer `mse` of the same major, if there is one, and continues with it;
4. applies the config: secrets, landing page, healthchecks, images, containers, wiring, timers (see [Commands](COMMANDS.md#mse-apply));
5. pings its healthchecks.io check.
