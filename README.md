# media-server-engine

A self-hosted media server stack (Jellyfin, Sonarr, Radarr, Prowlarr, Bazarr, Deluge behind a VPN, Seerr, Maintainerr, Portainer, Configarr) packaged as an engine. Each installation keeps its own settings, image versions and encrypted secrets in a separate config repository; the engine provides the compose files, scripts, timers and tests that run it.
