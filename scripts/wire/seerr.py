import json
import os

from wirelib import DATA_DIR, DRY_RUN, Api, WiringError, app_secrets, declared, report, run

APP = "seerr"
NOT_CONFIGURED = 4
JELLYFIN = 2
SETTINGS_FILE = os.path.join(DATA_DIR, "volumes", "seerr", "config", "settings.json")
ARRS = {
    "sonarr": {"name": "Sonarr", "hostname": "sonarr", "port": 8989, "key": "SONARR_API_KEY"},
    "radarr": {"name": "Radarr", "hostname": "radarr", "port": 7878, "key": "RADARR_API_KEY"},
}


def seerr_api_key():
    try:
        with open(SETTINGS_FILE) as settings:
            return json.load(settings)["main"]["apiKey"]
    except (FileNotFoundError, KeyError, ValueError):
        raise WiringError(f"no API key in {SETTINGS_FILE}; start seerr once so it writes its settings") from None


class Seerr:
    def __init__(self, api, secrets):
        self.api = api
        self.secrets = secrets
        self.failures = []

    def needs_first_sign_in(self):
        return self.api.get("/api/v1/settings/public").get("mediaServerType") == NOT_CONFIGURED

    def sign_in_with_jellyfin(self, admin_user):
        report(APP, f"sign in with jellyfin as {admin_user}")
        self.api.write("POST", "/api/v1/auth/jellyfin", {
            "username": admin_user,
            "password": self.secrets["JELLYFIN_ADMIN_PASSWORD"],
            "hostname": "jellyfin",
            "port": 8096,
            "useSsl": False,
            "urlBase": "",
            "serverType": JELLYFIN,
        })

    def wire_libraries(self, names):
        libraries = self.api.get("/api/v1/settings/jellyfin/library")
        if any(name not in {library["name"] for library in libraries} for name in names):
            report(APP, "sync libraries from jellyfin")
            libraries = self.api.write("POST", "/api/v1/settings/jellyfin/library/sync") or []
        by_name = {library["name"]: library for library in libraries}
        for name in names:
            library = by_name.get(name)
            if library is None:
                if DRY_RUN:
                    report(APP, f"enable library {name}")
                else:
                    self.failures.append(f"jellyfin has no library {name}")
                continue
            if not library.get("enabled"):
                report(APP, f"enable library {name}")
                self.api.write("PUT", f"/api/v1/settings/jellyfin/library/{library['id']}", {"enabled": True})

    def wire_external_url(self, url):
        settings = self.api.get("/api/v1/settings/jellyfin")
        if settings.get("externalHostname") != url:
            report(APP, f"set jellyfin external url {settings.get('externalHostname') or '(none)'} -> {url}")
            settings["externalHostname"] = url
            self.api.write("POST", "/api/v1/settings/jellyfin", settings)

    def profile_and_folder(self, kind, arr, key, declared_arr):
        found = self.api.request("POST", f"/api/v1/settings/{kind}/test", {
            "hostname": arr["hostname"], "port": arr["port"], "apiKey": key, "useSsl": False, "baseUrl": "",
        })
        profile = next((p for p in found["profiles"] if p["name"] == declared_arr["quality_profile"]), None)
        if profile is None:
            raise WiringError(f"{kind} has no quality profile {declared_arr['quality_profile']}")
        if declared_arr["root_folder"] not in {folder["path"] for folder in found["rootFolders"]}:
            raise WiringError(f"{kind} has no root folder {declared_arr['root_folder']}")
        return profile

    def set_profile_and_folder(self, entry, kind, profile, folder):
        entry.update(activeProfileId=profile["id"], activeProfileName=profile["name"], activeDirectory=folder)
        if kind == "sonarr":
            entry.update(activeAnimeProfileId=profile["id"], activeAnimeProfileName=profile["name"], activeAnimeDirectory=folder)

    def wire_arr(self, kind, declared_arr):
        arr = ARRS[kind]
        key = self.secrets[arr["key"]]
        entries = self.api.get(f"/api/v1/settings/{kind}")
        entry = next((e for e in entries if e.get("isDefault") and not e.get("is4k")), entries[0] if entries else None)
        if entry is None:
            report(APP, f"add {kind} with quality profile {declared_arr['quality_profile']} and root folder {declared_arr['root_folder']}")
            if DRY_RUN:
                return
            entry = {
                "name": arr["name"], "hostname": arr["hostname"], "port": arr["port"], "apiKey": key,
                "useSsl": False, "baseUrl": "", "is4k": False, "isDefault": True, "syncEnabled": True,
                "preventSearch": False, "tags": [],
            }
            if kind == "sonarr":
                entry.update(enableSeasonFolders=declared_arr.get("season_folders", False), animeTags=[])
            else:
                entry["minimumAvailability"] = declared_arr.get("minimum_availability", "released")
            self.set_profile_and_folder(entry, kind, self.profile_and_folder(kind, arr, key, declared_arr), declared_arr["root_folder"])
            self.api.write("POST", f"/api/v1/settings/{kind}", entry)
            return
        dirty = False
        for field, value in (("hostname", arr["hostname"]), ("port", arr["port"])):
            if entry.get(field) != value:
                report(APP, f"set {kind} {field} {entry.get(field)} -> {value}")
                entry[field] = value
                dirty = True
        if entry.get("apiKey") != key:
            report(APP, f"set {kind} api key")
            entry["apiKey"] = key
            dirty = True
        if kind == "radarr" and "minimum_availability" in declared_arr and entry.get("minimumAvailability") != declared_arr["minimum_availability"]:
            report(APP, f"set radarr minimum availability {entry.get('minimumAvailability')} -> {declared_arr['minimum_availability']}")
            entry["minimumAvailability"] = declared_arr["minimum_availability"]
            dirty = True
        profile_differs = entry.get("activeProfileName") != declared_arr["quality_profile"]
        folder_differs = entry.get("activeDirectory") != declared_arr["root_folder"]
        if profile_differs:
            report(APP, f"set {kind} quality profile {entry.get('activeProfileName')} -> {declared_arr['quality_profile']}")
        if folder_differs:
            report(APP, f"set {kind} root folder {entry.get('activeDirectory')} -> {declared_arr['root_folder']}")
        if (profile_differs or folder_differs) and not DRY_RUN:
            self.set_profile_and_folder(entry, kind, self.profile_and_folder(kind, arr, key, declared_arr), declared_arr["root_folder"])
            dirty = True
        if dirty:
            body = {field: value for field, value in entry.items() if field != "id"}
            self.api.write("PUT", f"/api/v1/settings/{kind}/{entry['id']}", body)

    def initialise(self):
        if not self.api.get("/api/v1/settings/public").get("initialized"):
            report(APP, "initialise")
            self.api.write("POST", "/api/v1/settings/initialize")

    def attempt(self, step, *args):
        try:
            step(*args)
        except WiringError as error:
            self.failures.append(str(error))


def report_planned_setup(config):
    for name in config.get("libraries") or []:
        report(APP, f"enable library {name}")
    for kind in ARRS:
        if kind in config:
            report(APP, f"add {kind} with quality profile {config[kind]['quality_profile']} and root folder {config[kind]['root_folder']}")
    report(APP, "initialise")


def wire():
    config = declared("apps.yml").get("seerr") or {}
    api = Api(APP, os.environ.get("SEERR_URL", "http://localhost:5055"), {"X-Api-Key": seerr_api_key()})
    seerr = Seerr(api, app_secrets())
    if seerr.needs_first_sign_in():
        seerr.sign_in_with_jellyfin(os.environ["JELLYFIN_ADMIN_USER"])
        if DRY_RUN:
            report_planned_setup(config)
            return
    seerr.attempt(seerr.wire_libraries, config.get("libraries") or [])
    if config.get("jellyfin_external_url"):
        seerr.attempt(seerr.wire_external_url, config["jellyfin_external_url"])
    for kind in ARRS:
        if kind in config:
            seerr.attempt(seerr.wire_arr, kind, config[kind])
    if not seerr.failures:
        seerr.initialise()
    if seerr.failures:
        raise WiringError("; ".join(seerr.failures))


if __name__ == "__main__":
    run(APP, wire)
