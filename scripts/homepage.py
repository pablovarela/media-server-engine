import json
import os
import shutil
import sys

import yaml

ENGINE_DIR = os.environ["ENGINE_DIR"]
CONFIG_DIR = os.environ["CONFIG_DIR"]
DATA_DIR = os.environ["DATA_DIR"]
DEFAULTS = os.path.join(ENGINE_DIR, "homepage")
CUSTOM = os.path.join(CONFIG_DIR, "homepage")
CONFIG_FILES = ("settings.yaml", "services.yaml", "widgets.yaml", "bookmarks.yaml", "custom.css", "custom.js")


def read(path):
    try:
        with open(path) as source:
            return source.read()
    except FileNotFoundError:
        return ""


def dotenv(path):
    values = {}
    for line in read(path).splitlines():
        key, _, value = line.partition("=")
        if key:
            values[key] = value
    return values


def healthchecks_api_key():
    return dotenv(os.path.join(ENGINE_DIR, ".secrets", "healthchecks.env")).get("HEALTHCHECKS_API_KEY", "")


def placeholders(text):
    for name, value in (
        ("INSTALLATION_NAME", os.environ["INSTALLATION_NAME"]),
        ("ENGINE_VERSION", os.environ.get("HOMEPAGE_ENGINE_VERSION", "")),
        ("HOST", os.environ["HOMEPAGE_HOST"]),
    ):
        text = text.replace(f"@{name}@", value)
    return text


def without_backup_status(services_yaml):
    groups = yaml.safe_load(services_yaml) or []
    for group in groups:
        for name, services in group.items():
            group[name] = [s for s in services if "Healthchecks" not in s]
    return yaml.safe_dump(groups, sort_keys=False)


def render(out):
    os.makedirs(out, exist_ok=True)
    for name in CONFIG_FILES:
        if os.path.exists(os.path.join(out, name)):
            os.remove(os.path.join(out, name))
    if os.path.isdir(CUSTOM):
        for name in CONFIG_FILES:
            if os.path.exists(os.path.join(CUSTOM, name)):
                shutil.copyfile(os.path.join(CUSTOM, name), os.path.join(out, name))
        if not os.path.exists(os.path.join(out, "bookmarks.yaml")):
            shutil.copyfile(os.path.join(DEFAULTS, "bookmarks.yaml"), os.path.join(out, "bookmarks.yaml"))
        return
    for name in ("settings.yaml", "widgets.yaml", "services.yaml", "bookmarks.yaml"):
        text = placeholders(read(os.path.join(DEFAULTS, name)))
        if name == "services.yaml" and not healthchecks_api_key():
            text = without_backup_status(text)
        with open(os.path.join(out, name), "w") as rendered:
            rendered.write(text)


def seerr_key():
    try:
        return json.loads(read(os.path.join(DATA_DIR, "volumes", "seerr", "config", "settings.json")))["main"]["apiKey"]
    except (ValueError, KeyError):
        return ""


def bazarr_key():
    config = yaml.safe_load(read(os.path.join(DATA_DIR, "volumes", "bazarr", "config", "config", "config.yaml"))) or {}
    return (config.get("auth") or {}).get("apikey", "")


def wiring_state(name):
    return read(os.path.join(DATA_DIR, "volumes", ".wiring", name)).strip()


def env():
    apps = dotenv(os.path.join(ENGINE_DIR, ".secrets", "apps.env"))
    for name, value in (
        ("SONARR_KEY", apps.get("SONARR_API_KEY", "")),
        ("RADARR_KEY", apps.get("RADARR_API_KEY", "")),
        ("PROWLARR_KEY", apps.get("PROWLARR_API_KEY", "")),
        ("DELUGE_PASSWORD", apps.get("DELUGE_WEB_PASSWORD", "")),
        ("JELLYFIN_KEY", wiring_state("jellyfin.key")),
        ("SEERR_KEY", seerr_key()),
        ("BAZARR_KEY", bazarr_key()),
        ("GLUETUN_KEY", wiring_state("gluetun-control.key")),
        ("HEALTHCHECKS_KEY", healthchecks_api_key()),
    ):
        print(f"HOMEPAGE_VAR_{name}={value}")


if __name__ == "__main__":
    if sys.argv[1] == "render":
        render(sys.argv[2])
    else:
        env()
