# Changing this installation's config

There are two ways to change the config, and they can be mixed:

- `make configure`, run from the installation's engine (`cd ~/<name>/engine`). It shows menus when whiptail is installed and asks plain questions otherwise. It covers the settings and secrets below.
- Editing the files in this repository by hand. Every setting is a file here, so nothing needs the configuration tool.

Either way, a change reaches the apps at the next `make update`.

## Plain files

Edit these with any editor.

| File | What it holds |
|---|---|
| `installation.env` | `TZ` (time zone), `CONFIG_ON_GITHUB` (`y` or `n`), `GITHUB_OWNER`, `JELLYFIN_ADMIN_USER`, `RESTIC_REPOSITORY` (a local path, or `b2:<bucket>:<folder>`). `INSTALLATION_NAME` is fixed once the installation exists: its directory, repository, backups and healthchecks are named after it. |
| `engine.env` | `ENGINE_VERSION`: the engine release to run, or `local` to run the engine checkout as it is. |
| `images.yml`, `images.monitoring.yml` | The image of every service, pinned to a digest. Renovate updates them. |
| `apps.yml` | Jellyfin's server name and libraries, Deluge's settings and plugins, Seerr's libraries and quality profiles, Bazarr's subtitle languages. |
| `prowlarr.yml` | Prowlarr's indexers, the FlareSolverr proxy and the links to Sonarr and Radarr. |
| `configarr/config.yml` | Sonarr's and Radarr's quality profiles, custom formats, root folders, download client and naming, applied by Configarr. |
| `compose.override.yml` | Optional additions or changes to the engine's compose file, for example an extra volume. |

## Secrets

The files in `secrets/` are encrypted with SOPS for the key in `.sops.yaml`. Open one with:

```
sops secrets/vpn.sops.env
```

SOPS decrypts it into your editor and encrypts it again when you save. It needs this installation's secrets key, which it reads from `~/.config/sops/age/keys.txt` (or the file in `SOPS_AGE_KEY_FILE`). Never save a decrypted copy inside this repository.

| File | Keys |
|---|---|
| `secrets/vpn.sops.env` | `VPN_SERVICE_PROVIDER`, `OPENVPN_USER`, `OPENVPN_PASSWORD`, `SERVER_COUNTRIES`. Any other gluetun setting can be added here too; `make configure` keeps keys it does not manage. |
| `secrets/backup.sops.env` | `RESTIC_PASSWORD`, and for B2 `B2_ACCOUNT_ID` and `B2_ACCOUNT_KEY`. |
| `secrets/healthchecks.sops.env` | `HEALTHCHECKS_PING_KEY`, empty to turn the pings off. |
| `secrets/apps.sops.env` | `JELLYFIN_ADMIN_PASSWORD`, `DELUGE_WEB_PASSWORD`, `PORTAINER_ADMIN_PASSWORD` (at least 12 characters), and the internal `SONARR_API_KEY`, `RADARR_API_KEY` and `PROWLARR_API_KEY`. |

The internal API keys connect the apps to each other. Change one with `make configure ROTATE=sonarr` (or `radarr`, `prowlarr`) rather than by hand, so every app that uses it is rewired.

Deluge's web password is applied at every update. Jellyfin's and Portainer's admin passwords are used when those apps are first set up; to change one later, change it in the app as well as here.

## Applying a change

Commit it:

```
git -C ~/<name>/config commit -am "What changed"
```

For a config kept on GitHub, push the commit. Every machine of the installation applies it at its next daily update, or straight away with `make update` from its engine. For a local-only config, run `make update` from `~/<name>/engine`.

`make update` refuses to run while the config has changes that are not committed, and lists them.
