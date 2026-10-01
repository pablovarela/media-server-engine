# Upgrading

An installation runs exactly what its config pins: the engine release in `engine.env`, and every image, by tag and digest, in `images.yml` and `images.monitoring.yml`. Nothing changes until the config changes.

## With Renovate

Add the config repository to the Renovate app, and the engine repository too when it is private: Renovate finds engine releases among the engine repository's tags and cannot see them otherwise. It opens a pull request for each new image and each new engine release, and keeps a Dependency Dashboard issue listing them all. Merging a pull request is the upgrade: every machine of the installation applies it at its next update, daily at 05:00 with the timer, or straight away with `make update`.

Before merging an engine release, read its release notes on GitHub; they say when a config needs new files or settings.

## By hand

Change `ENGINE_VERSION` or an image in the config, commit, push if the config is on GitHub, and run `make update`. Images must be pinned to a digest; `make update` refuses an image without one.

`ENGINE_VERSION=local` runs the engine as it is checked out, without switching. Update it with `git -C ~/<name>/engine pull`.

## Going back

Revert the commit that made the change, in the config repository, and run `make update`: the engine switches back to the previous release, and the previous images are pulled again. App data is not migrated back by this: if an app upgraded its database, restore the backup taken before the upgrade (see [Restoring](RESTORE.md)).

## What update does

1. Refuses if the engine or the config has uncommitted changes, and shows them.
2. Pulls the config, if it has a remote.
3. Switches the engine to the release in `engine.env`, if it differs, and continues with the new engine.
4. Checks that every image is pinned and that every app that writes state runs as uid and gid 1000.
5. Pulls the images, starts the apps and removes containers no longer declared.
6. Wires the apps, printing one line per change.
7. Removes images no longer pinned.
