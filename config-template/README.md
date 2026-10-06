# Media server installation config

The configuration of one media server installation, used by [media-server-engine](https://github.com/ENGINE_REPOSITORY). Each machine of the installation keeps a clone of it and applies it with `mse apply`, and at its nightly update.

| File | Content |
|---|---|
| `installation.env` | installation name, time zone, Jellyfin admin user, backup repository; edited with `mse configure` |
| `images.yml`, `images.monitoring.yml` | the image of every service, pinned to a digest |
| `compose.override.yml` | optional additions or changes to the engine's compose file |
| `homepage/` | the landing page, in Homepage's own format; starts as a copy of the engine's default page |
| `configarr/config.yml` | Sonarr and Radarr quality profiles, custom formats, naming, root folders, download client |
| `prowlarr.yml` | Prowlarr indexers, their priorities, the FlareSolverr proxy and the links to Sonarr and Radarr |
| `apps.yml` | Jellyfin server name and libraries, and the other apps' declared settings |
| `secrets/*.sops.env` | VPN, backup, healthchecks and app credentials, encrypted with SOPS for the key in `.sops.yaml` |
| `renovate.json` | Renovate opens a pull request for every image update |

Change settings and secrets with `mse configure`, or edit any file by hand: [CONFIG.md](CONFIG.md) explains every file and how to edit the encrypted secrets. A config kept on GitHub is pushed and merged changes reach every machine of the installation at its next nightly update, or straight away with `mse update --apply`; a local-only config is used as it is by the one machine that has it.

## prowlarr.yml

```yaml
indexer_proxies:
  - name: FlareSolverr
    host: http://localhost:8191/     # FlareSolverr shares Prowlarr's network
indexers:
  - name: The Pirate Bay             # the name shown in Prowlarr
    definition: thepiratebay         # the indexer definition to create it from
    priority: 25                     # optional
    proxy: FlareSolverr              # optional, one of indexer_proxies
    fields:                          # optional, the definition's own settings
      apiurl: apibay.org
applications:
  - name: Sonarr
    url: http://sonarr:8989
    api_key: SONARR_API_KEY          # the name of the key in the app secrets
    sync_categories: [5000, 5040]    # optional
```

`mse apply` creates what is missing and corrects declared values that differ. It never deletes an indexer, proxy or application, and leaves settings that are not declared as they are.

## apps.yml

```yaml
jellyfin:
  server_name: Media
  libraries:                         # created; a library's locations become exactly its path
    - name: Shows
      type: tvshows
      path: /data/media/tvshows
deluge:
  core:                              # any core.conf setting
    max_upload_speed: 2000.0
    download_location: /data/downloads
  plugins:                           # enabled; plugins enabled by hand stay enabled
    - name: Label                    # Sonarr and Radarr need it for categories
    - name: AutoRemovePlus
      source: https://...tar.gz      # built in the container when its egg is missing
      sha256: ...
      settings: {}                   # written to the plugin's own conf file
seerr:
  libraries: [Shows, Movies]         # Jellyfin libraries Seerr shows
  jellyfin_external_url: http://...  # optional, the Jellyfin link Seerr gives users
  sonarr:
    quality_profile: WEB-1080p       # by name, as Configarr creates it
    root_folder: /data/media/tvshows
    season_folders: false            # optional, used when Sonarr is added
  radarr:
    quality_profile: HD Bluray + WEB
    root_folder: /data/media/movies
    minimum_availability: released
bazarr:
  languages: [en]                    # subtitle languages
```

Deluge reads its settings at start, so when any of them differ `mse apply` stops Deluge, writes them and starts it again. The web password comes from the app secrets.

Bazarr's default subtitle profile for series and movies gets the declared languages; a Bazarr without one gets a profile named Default. Other profiles are kept.

Sonarr, Radarr and Prowlarr need no login from the local network. From anywhere else they ask for one, and none exists, so they stay closed. Devices on Tailscale count as local when they reach the network through a subnet router; otherwise turn on Trust CGNAT IP addresses in each app's security settings.

Sonarr and Radarr tell Jellyfin about every import, upgrade, rename and deletion, so new episodes and films appear in Jellyfin about a minute after they are imported. The one exception is the very first title in a new installation: Jellyfin only acts on these updates once it has scanned a library with something in it, so that first title appears at the next scheduled library scan (every 12 hours by default), or straight away with Scan All Libraries in Jellyfin's dashboard. Everything after it follows within the minute.

A new Seerr is signed in with the Jellyfin admin account from `installation.env` and the app secrets, which makes that account Seerr's owner.
