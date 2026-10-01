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


def declared(name):
    text = read(os.path.join(CONFIG_DIR, "homepage", name))
    return yaml.safe_load(text) if name.endswith(".yaml") else text


def merged(default, override):
    if isinstance(default, dict) and isinstance(override, dict):
        result = dict(default)
        for key, value in override.items():
            result[key] = merged(default.get(key), value)
        return result
    return override


def name_of(entry):
    return next(iter(entry))


def merged_by_name(defaults, declared_entries, merge_values):
    order = [name_of(entry) for entry in defaults]
    values = {name_of(entry): entry[name_of(entry)] for entry in defaults}
    for entry in declared_entries or []:
        name = name_of(entry)
        if name not in values:
            order.append(name)
            values[name] = entry[name]
        else:
            values[name] = merge_values(values[name], entry[name])
    return [{name: values[name]} for name in order if values[name] is not None]


def rendered_settings(text):
    settings = yaml.safe_load(text) or {}
    wanted = declared("settings.yaml") or {}
    declared_layout = wanted.pop("layout", None) or []
    settings = merged(settings, wanted)
    layout = settings.get("layout") or []
    defaults = {name_of(entry): entry[name_of(entry)] for entry in layout}
    first = [{name_of(e): merged(defaults.get(name_of(e)), e[name_of(e)])} for e in declared_layout]
    named_first = {name_of(e) for e in first}
    settings["layout"] = first + [e for e in layout if name_of(e) not in named_first]
    return yaml.safe_dump(settings, sort_keys=False)


def rendered_services(text):
    groups = yaml.safe_load(text) or []
    if not healthchecks_api_key():
        groups = [{name: [s for s in tiles if "Healthchecks" not in s]} for g in groups for name, tiles in g.items()]
    merge_tiles = lambda default_tiles, tiles: merged_by_name(default_tiles or [], tiles, merged)
    return yaml.safe_dump(merged_by_name(groups, declared("services.yaml"), merge_tiles), sort_keys=False)


def rendered_widgets(text):
    widgets = declared("widgets.yaml") or yaml.safe_load(text) or []
    for widget in widgets:
        for options in widget.values():
            if isinstance(options, dict) and options.get("href") == "":
                options.pop("href")
                options.pop("target", None)
    return yaml.safe_dump(widgets, sort_keys=False)


def rendered_bookmarks(text):
    return yaml.safe_dump(declared("bookmarks.yaml") or yaml.safe_load(text) or [], sort_keys=False)


def custom_css():
    return read(os.path.join(DEFAULTS, "custom.css")) + declared("custom.css")


RENDERERS = {
    "settings.yaml": rendered_settings,
    "services.yaml": rendered_services,
    "widgets.yaml": rendered_widgets,
    "bookmarks.yaml": rendered_bookmarks,
}


def render(out):
    os.makedirs(out, exist_ok=True)
    for name, rendered in RENDERERS.items():
        with open(os.path.join(out, name), "w") as target:
            target.write(rendered(placeholders(read(os.path.join(DEFAULTS, name)))))
    with open(os.path.join(out, "custom.css"), "w") as css:
        css.write(custom_css())


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
