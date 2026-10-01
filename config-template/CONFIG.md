# Changing this installation's config

There are two ways to change the config, and they can be mixed:

- `make configure`, run from the installation (`cd ~/<name>`). It shows menus when whiptail is installed and asks plain questions otherwise. It covers the settings and secrets below, and checks them: the time zone must exist, a backup folder must be writable and Backblaze B2 must accept the key, bucket and password.
- Editing the files in this repository by hand. Every setting is a file here, so nothing needs the configuration tool.

Either way, a change reaches the apps at the next `make update`.

## Plain files

Edit these with any editor.

| File | What it holds |
|---|---|
| `installation.env` | `TZ` (a time zone such as `Europe/London`), `CONFIG_LOCATION` (`local` or `github`), `GITHUB_OWNER`, `JELLYFIN_ADMIN_USER`, `RESTIC_REPOSITORY` (an absolute folder path, `b2:<bucket>:<folder>` for Backblaze B2, or any other repository restic takes, such as `sftp:` or `s3:`, with its credentials added to `secrets/backup.sops.env`). `INSTALLATION_NAME` is fixed once the installation exists: its directory, repository, backups and healthchecks are named after it. Keys and comments added by hand are kept by `make configure`: `MEDIA_SERVER_HOST` sets the name the app addresses use, and `HOMEPAGE_ALLOWED_HOSTS` adds names the landing page answers to, comma separated (for example a Tailscale name), next to this machine's own. |
| `engine.env` | `ENGINE_VERSION`: the engine release to run, or `local` to run the engine checkout as it is. |
| `images.yml`, `images.monitoring.yml` | The image of every service, pinned to a digest. Renovate updates them. The landing page (`homepage`) runs only while its image is listed here. |
| `homepage/` | Optional: the landing page's own `settings.yaml`, `services.yaml` and `widgets.yaml`. Without this folder the engine's default page is used, and it improves with engine releases. `make homepage-customize` copies the page in use here, after which these files are used as they are. |
| `apps.yml` | Jellyfin's server name and libraries, Deluge's settings and plugins, Seerr's libraries and quality profiles, Bazarr's subtitle languages. |
| `prowlarr.yml` | Prowlarr's indexers, the FlareSolverr proxy and the links to Sonarr and Radarr. |
| `configarr/config.yml` | Sonarr's and Radarr's quality profiles, custom formats, root folders, download client and naming, applied by Configarr. `!secret NAME` refers to a key in `secrets/apps.sops.env`; Configarr is given only the keys referred to. |
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
| `secrets/healthchecks.sops.env` | `HEALTHCHECKS_PING_KEY`, empty to turn the pings off. `HEALTHCHECKS_API_KEY` (optional): a read-only API key of the same healthchecks.io project, to show the checks' status on the landing page. |
| `secrets/apps.sops.env` | `JELLYFIN_ADMIN_PASSWORD`, `DELUGE_WEB_PASSWORD`, `PORTAINER_ADMIN_PASSWORD` (at least 12 characters), and the internal `SONARR_API_KEY`, `RADARR_API_KEY` and `PROWLARR_API_KEY`. |

The internal API keys connect the apps to each other. Change one with `make configure ROTATE=sonarr` (or `radarr`, `prowlarr`) rather than by hand, so every app that uses it is rewired.

Deluge's web password is applied at every update. Jellyfin's and Portainer's admin passwords are used when those apps are first set up; to change one later, change it in the app as well as here.

## Applying a change

Commit it:

```
git -C ~/<name>/config commit -am "What changed"
```

For a config kept on GitHub, push the commit. Every machine of the installation applies it at its next daily update, or straight away with `make update` from `~/<name>`. For a local-only config, run `make update` from `~/<name>`.

`make update` refuses to run while the config has changes that are not committed, and lists them.
