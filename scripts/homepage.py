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
        ("ENGINE_URL", os.environ.get("HOMEPAGE_ENGINE_URL", "")),
    ):
        text = text.replace(f"@{name}@", value)
    return text


def page_file(name):
    declared = os.path.join(CONFIG_DIR, "homepage", name)
    source = declared if os.path.exists(declared) else os.path.join(DEFAULTS, name)
    return placeholders(read(source))


def without_backup_status(entries):
    kept = []
    for entry in entries:
        name, value = next(iter(entry.items()))
        if name == "Healthchecks":
            continue
        kept.append({name: without_backup_status(value) if isinstance(value, list) else value})
    return kept


def rendered_services(text):
    groups = yaml.safe_load(text) or []
    if not healthchecks_api_key():
        groups = without_backup_status(groups)
    return yaml.safe_dump(groups, sort_keys=False)


def rendered_widgets(text):
    widgets = yaml.safe_load(text) or []
    for widget in widgets:
        for options in widget.values():
            if isinstance(options, dict) and options.get("href") == "":
                options.pop("href")
                options.pop("target", None)
    return yaml.safe_dump(widgets, sort_keys=False)


def as_written(text):
    return text


RENDERERS = {
    "settings.yaml": as_written,
    "services.yaml": rendered_services,
    "widgets.yaml": rendered_widgets,
    "bookmarks.yaml": as_written,
    "custom.css": as_written,
}


def render(out):
    os.makedirs(out, exist_ok=True)
    for name, rendered in RENDERERS.items():
        with open(os.path.join(out, name), "w") as target:
            target.write(rendered(page_file(name)))


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
