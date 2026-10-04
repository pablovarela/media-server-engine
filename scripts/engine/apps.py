import os

from engine import commands, compose, installation, program, secrets

APPS = [
    ("Jellyfin", 8096), ("Seerr", 5055), ("Sonarr", 8989), ("Radarr", 7878), ("Prowlarr", 9696),
    ("Bazarr", 6767), ("Deluge", 8112), ("Maintainerr", 6246), ("Portainer", 9000),
]
NO_LOGIN = "no login on the local network"


def urls(argv):
    installation.load_installation()
    host = installation.network_name()
    if "homepage" in compose.optional_services_pinned().split(","):
        port = installation.homepage_port()
        print(f"{'Home':<12} http://{host}" + ("" if port == "80" else f":{port}"))
    for name, port in APPS:
        print(f"{name:<12} http://{host}:{port}")


def logins(argv):
    installation.load_installation()
    keys = secrets.dotenv(commands.output(["sops", "decrypt", "--output-type", "dotenv", os.path.join(installation.config_dir(), "secrets", "apps.sops.env")]))
    print(f"{'App':<12} {'User':<10} Password")
    print(f"{'Jellyfin':<12} {os.environ.get('JELLYFIN_ADMIN_USER', ''):<10} {keys.get('JELLYFIN_ADMIN_PASSWORD', '')}")
    print(f"{'Seerr':<12} sign in with the Jellyfin account")
    print(f"{'Deluge':<12} {'':<10} {keys.get('DELUGE_WEB_PASSWORD', '')}")
    print(f"{'Portainer':<12} {'admin':<10} {keys.get('PORTAINER_ADMIN_PASSWORD', '')}")
    for app in ("Sonarr", "Radarr", "Prowlarr"):
        print(f"{app:<12} {NO_LOGIN}")


def urls_main(argv):
    return program.run("urls", urls, argv)


def logins_main(argv):
    return program.run("logins", logins, argv)
