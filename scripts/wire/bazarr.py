import os

import yaml

from wirelib import DATA_DIR, Api, WiringError, app_secrets, report, run

APP = "bazarr"
CONFIG_FILE = os.path.join(DATA_DIR, "volumes", "bazarr", "config", "config", "config.yaml")
ARRS = {
    "sonarr": {"ip": "sonarr", "port": 8989, "key": "SONARR_API_KEY"},
    "radarr": {"ip": "radarr", "port": 7878, "key": "RADARR_API_KEY"},
}


def bazarr_api_key():
    try:
        with open(CONFIG_FILE) as config:
            return yaml.safe_load(config)["auth"]["apikey"]
    except (FileNotFoundError, KeyError, TypeError):
        raise WiringError(f"no API key in {CONFIG_FILE}; start bazarr once so it writes its config") from None


def form_value(value):
    if isinstance(value, bool):
        return "true" if value else "false"
    return str(value)


def wanted_changes(settings, secrets):
    changes = {}
    for kind, arr in ARRS.items():
        current = settings[kind]
        for field, value in (("ip", arr["ip"]), ("port", arr["port"]), ("base_url", "")):
            if form_value(current.get(field)) != form_value(value):
                report(APP, f"set {kind} {field} {current.get(field)} -> {value}")
                changes[f"settings-{kind}-{field}"] = form_value(value)
        if current.get("apikey") != secrets[arr["key"]]:
            report(APP, f"set {kind} api key")
            changes[f"settings-{kind}-apikey"] = secrets[arr["key"]]
        if settings["general"].get(f"use_{kind}") is not True:
            report(APP, f"turn on use_{kind}")
            changes[f"settings-general-use_{kind}"] = "true"
    return changes


def wire():
    api = Api(APP, os.environ.get("BAZARR_URL", "http://localhost:6767"), {"X-API-KEY": bazarr_api_key()})
    changes = wanted_changes(api.get("/api/system/settings"), app_secrets())
    if changes:
        api.write("POST", "/api/system/settings", form=changes)


if __name__ == "__main__":
    run(APP, wire)
