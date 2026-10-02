import json
import os
import re
import shutil
import sys
import urllib.request

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


HEALTHCHECK_MARKER = re.compile(r"@HEALTHCHECK_([A-Z]+)@")


def healthcheck_positions():
    url = os.environ.get("HEALTHCHECKS_API_URL", "https://healthchecks.io/api/v3/checks/")
    request = urllib.request.Request(url, headers={"X-Api-Key": healthchecks_api_key()})
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            checks = json.load(response)["checks"]
    except (OSError, ValueError, KeyError) as error:
        print(f"could not read the checks from healthchecks ({error}); the page is drawn without them", file=sys.stderr)
        return {}
    return {check.get("slug"): position for position, check in enumerate(checks)}


def with_found_checks(tile, positions):
    mappings = (tile.get("widget") or {}).get("mappings")
    if not isinstance(mappings, list):
        return tile
    kept = []
    for mapping in mappings:
        field = str(mapping.get("field", ""))
        marker = HEALTHCHECK_MARKER.search(field)
        if marker:
            position = positions.get(f"{os.environ['INSTALLATION_NAME']}-{marker.group(1).lower()}")
            if position is None:
                continue
            mapping = dict(mapping, field=field.replace(marker.group(0), str(position)))
        kept.append(mapping)
    if not kept:
        return None
    tile["widget"]["mappings"] = kept
    return tile


def with_health_checks(entries, positions):
    kept = []
    for entry in entries:
        name, value = next(iter(entry.items()))
        if isinstance(value, list):
            found = with_health_checks(value, positions)
            if value and not found:
                continue
            value = found
        elif isinstance(value, dict):
            value = with_found_checks(value, positions)
            if value is None:
                continue
        kept.append({name: value})
    return kept


def rendered_services(text):
    groups = yaml.safe_load(text) or []
    if not healthchecks_api_key():
        groups = without_backup_status(groups)
    elif HEALTHCHECK_MARKER.search(text):
        groups = with_health_checks(groups, healthcheck_positions())
    return yaml.safe_dump(groups, sort_keys=False, allow_unicode=True)


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


def copy_images(out):
    images = out.rstrip("/") + "-images"
    os.makedirs(images, exist_ok=True)
    for name in os.listdir(images):
        path = os.path.join(images, name)
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path)
        else:
            os.remove(path)
    declared = os.path.join(CONFIG_DIR, "homepage", "images")
    if os.path.isdir(declared):
        shutil.copytree(declared, images, dirs_exist_ok=True)


def render(out):
    os.makedirs(out, exist_ok=True)
    for name, rendered in RENDERERS.items():
        with open(os.path.join(out, name), "w") as target:
            target.write(rendered(page_file(name)))
    copy_images(out)


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
