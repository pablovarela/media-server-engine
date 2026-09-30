# Media server installation config

The configuration of one media server installation, used by [media-server-engine](https://github.com/ENGINE_REPOSITORY). The engine clones this repository next to itself and applies it with `make update`.

| File | Content |
|---|---|
| `installation.env` | installation name, time zone, GitHub owner, Jellyfin admin user, restic repository; written by `make configure` |
| `engine.env` | the engine version this installation runs |
| `images.yml`, `images.monitoring.yml` | the image of every service, pinned to a digest |
| `compose.override.yml` | optional additions or changes to the engine's compose file |
| `configarr/config.yml` | Sonarr and Radarr quality profiles, custom formats, naming, root folders, download client |
| `prowlarr.yml` | Prowlarr indexers, their priorities, the FlareSolverr proxy and the links to Sonarr and Radarr |
| `apps.yml` | Jellyfin server name and libraries, and the other apps' declared settings |
| `secrets/*.sops.env` | VPN, backup, healthchecks and app credentials, encrypted with SOPS for the key in `.sops.yaml` |
| `renovate.json` | Renovate opens a pull request for every image and engine update |

Change settings and secrets with `make configure` from the engine; edit the other files directly and push. Merged changes reach every machine of this installation at its next `make update`.

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

`make update` creates what is missing and corrects declared values that differ. It never deletes an indexer, proxy or application, and leaves settings that are not declared as they are.

## apps.yml

```yaml
jellyfin:
  server_name: Media
  libraries:                         # created, or given a missing path; never removed
    - name: Shows
      type: tvshows
      path: /data/tvshows
deluge:
  core:                              # any core.conf setting
    max_upload_speed: 2000.0
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
    root_folder: /tv
    season_folders: false            # optional, used when Sonarr is added
  radarr:
    quality_profile: HD Bluray + WEB
    root_folder: /movies
    minimum_availability: released
```

Deluge reads its settings at start, so when any of them differ `make update` stops Deluge, writes them and starts it again. The web password comes from the app secrets.

A new Seerr is signed in with the Jellyfin admin account from `installation.env` and the app secrets, which makes that account Seerr's owner.
