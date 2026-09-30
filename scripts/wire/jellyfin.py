import os
import urllib.parse

from wirelib import DRY_RUN, Api, WiringError, app_secrets, declared, remember, remembered, report, run

APP = "jellyfin"
KEY_NAME = "media-server"
KEY_STATE = "jellyfin.key"
CLIENT = 'MediaBrowser Client="media-server", Device="wiring", DeviceId="media-server-wiring", Version="1"'


class Jellyfin:
    def __init__(self, api, admin_user, admin_password):
        self.api = api
        self.admin_user = admin_user
        self.admin_password = admin_password
        self.failures = []

    def use_token(self, token):
        self.api.headers["Authorization"] = f'{CLIENT}, Token="{token}"'

    def wizard_completed(self):
        return self.api.get("/System/Info/Public").get("StartupWizardCompleted")

    def complete_wizard(self, server_name):
        report(APP, f"complete the startup wizard with admin user {self.admin_user}")
        configuration = self.api.get("/Startup/Configuration")
        configuration["ServerName"] = server_name
        self.api.write("POST", "/Startup/Configuration", configuration)
        self.api.get("/Startup/User")
        self.api.write("POST", "/Startup/User", {"Name": self.admin_user, "Password": self.admin_password})
        self.api.write("POST", "/Startup/RemoteAccess", {"EnableRemoteAccess": True, "EnableAutomaticPortMapping": False})
        self.api.write("POST", "/Startup/Complete")

    def sign_in(self):
        self.api.headers["Authorization"] = CLIENT
        try:
            session = self.api.request("POST", "/Users/AuthenticateByName", {"Username": self.admin_user, "Pw": self.admin_password})
        except WiringError as error:
            raise WiringError(f"cannot sign in as {self.admin_user}: {error}") from None
        self.use_token(session["AccessToken"])

    def stored_key_works(self, key):
        self.use_token(key)
        try:
            self.api.get("/System/Info")
            return True
        except WiringError:
            return False

    def media_server_key(self):
        keys = self.api.get("/Auth/Keys")["Items"]
        return next((k["AccessToken"] for k in keys if k["AppName"] == KEY_NAME), None)

    def ensure_key(self):
        stored = remembered(KEY_STATE)
        if stored and self.stored_key_works(stored):
            return
        self.sign_in()
        key = self.media_server_key()
        if key is None:
            report(APP, f"create API key {KEY_NAME}")
            self.api.write("POST", f"/Auth/Keys?app={KEY_NAME}")
            key = self.media_server_key()
            if key is None and DRY_RUN:
                return
        remember(KEY_STATE, key)
        self.use_token(key)

    def wire_server_name(self, server_name):
        configuration = self.api.get("/System/Configuration")
        if configuration.get("ServerName") != server_name:
            report(APP, f"set server name {configuration.get('ServerName')} -> {server_name}")
            configuration["ServerName"] = server_name
            self.api.write("POST", "/System/Configuration", configuration)

    def wire_libraries(self, libraries):
        current = {library["Name"]: library for library in self.api.get("/Library/VirtualFolders")}
        for library in libraries:
            name, kind, path = library["name"], library["type"], library["path"]
            existing = current.get(name)
            if existing is None:
                report(APP, f"add library {name}")
                query = urllib.parse.urlencode({"name": name, "collectionType": kind, "paths": path, "refreshLibrary": "false"})
                self.api.write("POST", f"/Library/VirtualFolders?{query}", {"LibraryOptions": {}})
                continue
            if existing.get("CollectionType") != kind:
                self.failures.append(f"library {name} is {existing.get('CollectionType')}, not {kind}; change it in Jellyfin")
                continue
            if path not in existing.get("Locations", []):
                report(APP, f"add {path} to library {name}")
                self.api.write("POST", "/Library/VirtualFolders/Paths?refreshLibrary=false", {"Name": name, "PathInfo": {"Path": path}})


def wire():
    config = declared("apps.yml").get("jellyfin") or {}
    admin_user = os.environ["JELLYFIN_ADMIN_USER"]
    api = Api(APP, os.environ.get("JELLYFIN_URL", "http://localhost:8096"), {})
    jellyfin = Jellyfin(api, admin_user, app_secrets()["JELLYFIN_ADMIN_PASSWORD"])
    server_name = config.get("server_name")
    libraries = config.get("libraries") or []
    if not jellyfin.wizard_completed():
        jellyfin.complete_wizard(server_name or "")
        if DRY_RUN:
            for library in libraries:
                report(APP, f"add library {library['name']}")
            return
    jellyfin.ensure_key()
    if server_name:
        jellyfin.wire_server_name(server_name)
    jellyfin.wire_libraries(libraries)
    if jellyfin.failures:
        raise WiringError("; ".join(jellyfin.failures))


if __name__ == "__main__":
    run(APP, wire)
