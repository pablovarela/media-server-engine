import json
import os

import yaml

from wirelib import DATA_DIR, Api, WiringError, app_secrets, declared, report, run

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


def wanted_changes(settings, secrets):
    changes = {}
    for kind, arr in ARRS.items():
        current = settings[kind]
        for field, value in (("ip", arr["ip"]), ("port", arr["port"]), ("base_url", "")):
            if str(current.get(field)) != str(value):
                report(APP, f"set {kind} {field} {current.get(field)} -> {value}")
                changes[f"settings-{kind}-{field}"] = str(value)
        if current.get("apikey") != secrets[arr["key"]]:
            report(APP, f"set {kind} api key")
            changes[f"settings-{kind}-apikey"] = secrets[arr["key"]]
        if settings["general"].get(f"use_{kind}") is not True:
            report(APP, f"turn on use_{kind}")
            changes[f"settings-general-use_{kind}"] = "true"
    return changes


def profile_items(languages, current_items):
    flags = {item["language"]: item for item in current_items}
    items = []
    for number, code in enumerate(languages, start=1):
        item = dict(flags.get(code) or {"audio_exclude": "False", "hi": "False", "forced": "False", "audio_only_include": "False"})
        item.update(id=number, language=code)
        items.append(item)
    return items


def default_profile(profiles, general):
    if general.get("serie_default_enabled") is not True:
        return None
    return next((p for p in profiles if str(p["profileId"]) == str(general.get("serie_default_profile"))), None)


def language_changes(api, general, languages):
    changes = {}
    known = api.get("/api/system/languages")
    unknown = [code for code in languages if code not in {language["code2"] for language in known}]
    if unknown:
        raise WiringError(f"bazarr has no language {', '.join(unknown)}")
    enabled = [language["code2"] for language in known if language["enabled"]]
    missing = [code for code in languages if code not in enabled]
    if missing:
        report(APP, f"enable languages {', '.join(missing)}")
        changes["languages-enabled"] = enabled + missing
    profiles = api.get("/api/system/languages/profiles")
    profile = default_profile(profiles, general)
    if profile is None:
        profile = {
            "profileId": max([p["profileId"] for p in profiles] + [0]) + 1, "name": "Default", "cutoff": None,
            "items": profile_items(languages, []), "mustContain": [], "mustNotContain": [], "originalFormat": 0, "tag": None,
        }
        report(APP, f"create subtitle profile Default with {', '.join(languages)}")
        changes["languages-profiles"] = json.dumps(profiles + [profile])
    else:
        current = [item["language"] for item in profile["items"]]
        if current != languages:
            report(APP, f"set subtitle profile {profile['name']} languages {', '.join(current)} -> {', '.join(languages)}")
            profile["items"] = profile_items(languages, profile["items"])
            changes["languages-profiles"] = json.dumps(profiles)
    for kind, label in (("serie", "series"), ("movie", "movies")):
        if general.get(f"{kind}_default_enabled") is not True:
            report(APP, f"use a default subtitle profile for {label}")
            changes[f"settings-general-{kind}_default_enabled"] = "true"
        current_id = general.get(f"{kind}_default_profile")
        if str(current_id) != str(profile["profileId"]):
            if general.get(f"{kind}_default_enabled") is True:
                current = next((p["name"] for p in profiles if str(p["profileId"]) == str(current_id)), current_id)
                report(APP, f"set the default subtitle profile for {label} {current} -> {profile['name']}")
            changes[f"settings-general-{kind}_default_profile"] = str(profile["profileId"])
    return changes


def wire():
    api = Api(APP, os.environ.get("BAZARR_URL", "http://localhost:6767"), {"X-API-KEY": bazarr_api_key()})
    settings = api.get("/api/system/settings")
    changes = wanted_changes(settings, app_secrets())
    languages = (declared("apps.yml").get("bazarr") or {}).get("languages")
    language_failure = None
    if languages:
        try:
            changes.update(language_changes(api, settings["general"], languages))
        except WiringError as error:
            language_failure = error
    if changes:
        api.write("POST", "/api/system/settings", form=changes)
    if language_failure:
        raise language_failure


if __name__ == "__main__":
    run(APP, wire)
