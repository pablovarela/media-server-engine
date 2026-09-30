import json
import os

from wirelib import DATA_DIR, Api, WiringError, app_secrets, remembered, report, run

APP = "maintainerr"
SEERR_SETTINGS = os.path.join(DATA_DIR, "volumes", "seerr", "config", "settings.json")
ARRS = {
    "sonarr": {"serverName": "Sonarr", "url": "http://sonarr:8989", "key": "SONARR_API_KEY"},
    "radarr": {"serverName": "Radarr", "url": "http://radarr:7878", "key": "RADARR_API_KEY"},
}


def seerr_api_key():
    try:
        with open(SEERR_SETTINGS) as settings:
            return json.load(settings)["main"]["apiKey"]
    except (FileNotFoundError, KeyError, ValueError):
        raise WiringError(f"no Seerr API key in {SEERR_SETTINGS}") from None


class Maintainerr:
    def __init__(self, api):
        self.api = api
        self.failures = []

    def save(self, method, path, body):
        answer = self.api.write(method, path, body)
        if isinstance(answer, dict) and answer.get("status") not in (None, "OK"):
            raise WiringError(f"{method} {path}: {answer.get('message')}")

    def connect(self, name, path, wanted):
        current = self.api.get(path)
        if any(current.get(key) != value for key, value in wanted.items()):
            report(APP, f"connect {name}")
            self.save("POST", path, wanted)

    def connect_arr(self, kind, key):
        arr = ARRS[kind]
        wanted = {"serverName": arr["serverName"], "url": arr["url"], "apiKey": key}
        entries = self.api.get(f"/api/settings/{kind}")
        entry = next((e for e in entries if e.get("url") == arr["url"] or e.get("serverName") == arr["serverName"]), None)
        if entry is None:
            report(APP, f"connect {kind}")
            self.save("POST", f"/api/settings/{kind}", wanted)
        elif any(entry.get(field) != value for field, value in wanted.items()):
            report(APP, f"reconnect {kind}")
            self.save("PUT", f"/api/settings/{kind}/{entry['id']}", wanted)

    def attempt(self, step, *args):
        try:
            step(*args)
        except WiringError as error:
            self.failures.append(str(error))


def jellyfin_key():
    key = remembered("jellyfin.key")
    if not key:
        raise WiringError("no Jellyfin key in data/volumes/.wiring/jellyfin.key; the jellyfin wiring stores it")
    return key


def wire():
    secrets = app_secrets()
    maintainerr = Maintainerr(Api(APP, os.environ.get("MAINTAINERR_URL", "http://localhost:6246"), {}))
    maintainerr.attempt(lambda: maintainerr.connect(
        "jellyfin", "/api/settings/jellyfin", {"jellyfin_url": "http://jellyfin:8096", "jellyfin_api_key": jellyfin_key()}))
    maintainerr.attempt(lambda: maintainerr.connect(
        "seerr", "/api/settings/seerr", {"url": "http://seerr:5055", "api_key": seerr_api_key()}))
    for kind, arr in ARRS.items():
        maintainerr.attempt(maintainerr.connect_arr, kind, secrets[arr["key"]])
    if maintainerr.failures:
        raise WiringError("; ".join(maintainerr.failures))


if __name__ == "__main__":
    run(APP, wire)
