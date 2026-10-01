import copy
import os

from wirelib import DRY_RUN, Api, WiringError, app_secrets, fingerprint, remember, remembered, report, run

APP = "library-updates"
NAME = "Emby / Jellyfin"
EVENTS_LEFT_OFF = {"onHealthIssue", "onHealthRestored", "onManualInteractionRequired"}
ARRS = {
    "sonarr": ("SONARR_URL", "http://localhost:8989", "SONARR_API_KEY", "/tv", "/data/tvshows"),
    "radarr": ("RADARR_URL", "http://localhost:7878", "RADARR_API_KEY", "/movies", "/data/movies"),
}


def wanted_fields(kind):
    arr_path, jellyfin_path = ARRS[kind][3:]
    return {"host": "jellyfin", "port": 8096, "updateLibrary": True, "mapFrom": arr_path, "mapTo": jellyfin_path}


def field(item, name):
    return next((f.get("value") for f in item["fields"] if f["name"] == name), None)


def set_field(item, name, value):
    next(f for f in item["fields"] if f["name"] == name)["value"] = value


def jellyfin_key():
    key = remembered("jellyfin.key")
    if not key:
        raise WiringError("no Jellyfin key in data/volumes/.wiring/jellyfin.key; the jellyfin wiring stores it")
    return key


def connect(kind, api, key):
    key_state = f"{kind}-jellyfin-connection.sha256"
    current = next((n for n in api.get("/api/v3/notification") if n["implementation"] == "MediaBrowser"), None)
    if current is None:
        item = copy.deepcopy(next(s for s in api.get("/api/v3/notification/schema") if s["implementation"] == "MediaBrowser"))
        item["name"] = NAME
        for event in [k for k in item if k.startswith("on")]:
            item[event] = event not in EVENTS_LEFT_OFF
        for name, value in wanted_fields(kind).items():
            set_field(item, name, value)
        set_field(item, "apiKey", key)
        report(kind, "add the Jellyfin connection")
        api.write("POST", "/api/v3/notification?forceSave=true", item)
        remember(key_state, fingerprint(key))
        return
    dirty = False
    for name, value in wanted_fields(kind).items():
        if field(current, name) != value:
            report(kind, f"set the Jellyfin connection {name} {field(current, name)} -> {value}")
            set_field(current, name, value)
            dirty = True
    if remembered(key_state) != fingerprint(key):
        report(kind, "set the Jellyfin connection api key")
        dirty = True
    if dirty:
        set_field(current, "apiKey", key)
        api.write("PUT", f"/api/v3/notification/{current['id']}?forceSave=true", current)
        remember(key_state, fingerprint(key))


def wire():
    secrets = app_secrets()
    if DRY_RUN and not remembered("jellyfin.key"):
        report(APP, "connect Sonarr and Radarr to Jellyfin once the jellyfin wiring has stored its key")
        return
    key = jellyfin_key()
    failures = []
    for kind, (url_variable, default_url, api_key, _, _) in ARRS.items():
        try:
            connect(kind, Api(kind, os.environ.get(url_variable, default_url), {"X-Api-Key": secrets[api_key]}), key)
        except WiringError as error:
            failures.append(f"{kind}: {error}")
    if failures:
        raise WiringError("; ".join(failures))


if __name__ == "__main__":
    run(APP, wire)
