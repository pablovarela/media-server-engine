import json
import os
import sys

import yaml

ENGINE_DIR = os.environ["ENGINE_DIR"]
CONFIG_DIR = os.environ["CONFIG_DIR"]
DATA_DIR = os.environ["DATA_DIR"]
DEFAULTS = os.path.join(ENGINE_DIR, "homepage")


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


def landing_page_settings():
    config = yaml.safe_load(read(os.path.join(CONFIG_DIR, "apps.yml"))) or {}
    return config.get("homepage") or {}


def rendered_settings(text, wanted):
    settings = yaml.safe_load(text) or {}
    for key in ("theme", "color"):
        if wanted.get(key):
            settings[key] = wanted[key]
    if wanted.get("links"):
        settings.setdefault("layout", []).append({"Links": {"style": "row", "columns": 4}})
    return yaml.safe_dump(settings, sort_keys=False)


def rendered_services(text, wanted):
    hidden = set(wanted.get("hidden") or [])
    if not healthchecks_api_key():
        hidden.add("Healthchecks")
    groups = yaml.safe_load(text) or []
    for group in groups:
        for name, services in group.items():
            group[name] = [s for s in services if not hidden & set(s)]
    links = [{link["name"]: {k: v for k, v in link.items() if k != "name"}} for link in wanted.get("links") or []]
    if links:
        groups.append({"Links": links})
    return yaml.safe_dump(groups, sort_keys=False)


def render(out):
    os.makedirs(out, exist_ok=True)
    wanted = landing_page_settings()
    for name in ("settings.yaml", "widgets.yaml", "services.yaml", "bookmarks.yaml"):
        text = placeholders(read(os.path.join(DEFAULTS, name)))
        if name == "settings.yaml":
            text = rendered_settings(text, wanted)
        elif name == "services.yaml":
            text = rendered_services(text, wanted)
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
