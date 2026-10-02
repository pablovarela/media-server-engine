import copy
import json

import pytest

ENGLISH_PROFILE = {
    "profileId": 1, "name": "English", "cutoff": None, "mustContain": [], "mustNotContain": [], "originalFormat": 0, "tag": "english",
    "items": [{"id": 1, "language": "en", "audio_exclude": "False", "hi": "False", "forced": "False", "audio_only_include": "False"}],
}
LANGUAGES = [{"code2": "en", "name": "English", "enabled": False}, {"code2": "es", "name": "Spanish", "enabled": False}, {"code2": "fr", "name": "French", "enabled": False}]


def fresh_settings():
    arr = {"ip": "127.0.0.1", "base_url": "/", "ssl": False, "apikey": "", "only_monitored": False}
    return {
        "general": {"use_sonarr": False, "use_radarr": False, "serie_default_enabled": False, "serie_default_profile": "", "movie_default_enabled": False, "movie_default_profile": ""},
        "sonarr": dict(arr, port=8989),
        "radarr": dict(arr, port=7878),
    }


def wired_settings():
    arr = {"base_url": "", "ssl": False, "only_monitored": False}
    return {
        "general": {"use_sonarr": True, "use_radarr": True, "serie_default_enabled": True, "serie_default_profile": 1, "movie_default_enabled": True, "movie_default_profile": 1},
        "sonarr": dict(arr, ip="sonarr", port=8989, apikey="sonarr-key"),
        "radarr": dict(arr, ip="radarr", port=7878, apikey="radarr-key"),
    }


@pytest.fixture
def bazarr(wiring, dirs, monkeypatch):
    config = dirs.data / "volumes" / "bazarr" / "config" / "config"
    config.mkdir(parents=True)
    (config / "config.yaml").write_text("auth:\n  apikey: bazarr-key\n")
    (dirs.config / "apps.yml").write_text("bazarr:\n  languages: [en]\n")
    monkeypatch.setenv("BAZARR_URL", "http://bazarr")
    return wiring("bazarr", "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n")


def answering(http, settings, profiles=(), enabled=()):
    languages = [dict(language, enabled=language["code2"] in enabled) for language in LANGUAGES]
    http.on("GET", "/api/system/settings", settings)
    http.on("GET", "/api/system/languages", languages)
    http.on("GET", "/api/system/languages/profiles", [copy.deepcopy(profile) for profile in profiles])
    http.on("POST", "/api/system/settings", None)


def fresh(http):
    answering(http, fresh_settings())


def wired(http, profiles=(ENGLISH_PROFILE,), settings=None):
    answering(http, settings or wired_settings(), profiles=profiles, enabled=("en",))


def posted(http):
    return http.body("POST", "/api/system/settings")


def profiles_posted(http):
    return [(p["profileId"], p["name"], [item["language"] for item in p["items"]], p["tag"]) for p in json.loads(posted(http)["languages-profiles"])]


def test_a_fresh_bazarr_is_connected_to_sonarr_and_radarr_in_one_write(bazarr, http, capsys):
    fresh(http)
    bazarr.wire()
    assert http.writes() == ["POST /api/system/settings"]
    form = posted(http)
    assert (form["settings-sonarr-ip"], form["settings-sonarr-base_url"]) == ("sonarr", "")
    assert (form["settings-radarr-ip"], form["settings-radarr-apikey"]) == ("radarr", "radarr-key")
    assert (form["settings-general-use_sonarr"], form["settings-general-use_radarr"]) == ("true", "true")
    reported = capsys.readouterr().out
    assert "bazarr: set sonarr ip 127.0.0.1 -> sonarr" in reported
    assert "sonarr-key" not in reported


def test_the_wiring_signs_its_requests_with_bazarrs_own_key(bazarr, http):
    wired(http)
    bazarr.wire()
    assert {request.headers["X-api-key"] for request in http.requests} == {"bazarr-key"}


def test_an_already_wired_bazarr_is_left_untouched(bazarr, http, capsys):
    wired(http)
    bazarr.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_a_rotated_radarr_key_is_sent_alone(wiring, bazarr, http, dirs, capsys):
    bazarr = wiring("bazarr", "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=rotated\n")
    wired(http)
    bazarr.wire()
    assert posted(http) == {"settings-radarr-apikey": "rotated"}
    assert "bazarr: set radarr api key" in capsys.readouterr().out


def test_a_dry_run_reports_the_changes_and_writes_nothing(wiring, bazarr, http, capsys):
    bazarr = wiring("bazarr", "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n", dry_run=True)
    fresh(http)
    bazarr.wire()
    assert http.writes() == []
    assert "(dry run) bazarr: turn on use_sonarr" in capsys.readouterr().out


def test_a_bazarr_that_has_not_written_its_config_yet_fails_with_a_hint(bazarr, http, dirs):
    (dirs.data / "volumes" / "bazarr" / "config" / "config" / "config.yaml").unlink()
    with pytest.raises(bazarr.WiringError, match="no API key in .*config.yaml; start bazarr once"):
        bazarr.wire()
    assert http.requests == []


def test_a_fresh_bazarr_gets_the_declared_languages_in_a_default_profile_for_series_and_movies(bazarr, http, capsys):
    fresh(http)
    bazarr.wire()
    form = posted(http)
    assert form["languages-enabled"] == "en"
    assert profiles_posted(http) == [(1, "Default", ["en"], None)]
    assert (form["settings-general-serie_default_enabled"], form["settings-general-movie_default_enabled"]) == ("true", "true")
    assert (form["settings-general-serie_default_profile"], form["settings-general-movie_default_profile"]) == ("1", "1")
    assert "bazarr: create subtitle profile Default with en" in capsys.readouterr().out


def test_an_added_language_joins_the_default_profile_which_keeps_its_name_tag_and_flags(bazarr, http, dirs, capsys):
    (dirs.config / "apps.yml").write_text("bazarr:\n  languages: [en, es]\n")
    hearing_impaired = copy.deepcopy(ENGLISH_PROFILE)
    hearing_impaired["items"][0]["hi"] = "True"
    wired(http, profiles=[hearing_impaired])
    bazarr.wire()
    form = posted(http)
    assert form["languages-enabled"] == ["en", "es"]
    [profile] = json.loads(form["languages-profiles"])
    assert (profile["name"], profile["tag"]) == ("English", "english")
    assert [(item["id"], item["language"], item["hi"]) for item in profile["items"]] == [(1, "en", "True"), (2, "es", "False")]
    assert "settings-general-serie_default_profile" not in form
    assert "bazarr: set subtitle profile English languages en -> en, es" in capsys.readouterr().out


def test_other_subtitle_profiles_are_sent_back_unchanged_since_bazarr_deletes_any_it_is_not_sent(bazarr, http, dirs):
    (dirs.config / "apps.yml").write_text("bazarr:\n  languages: [en, fr]\n")
    other = dict(copy.deepcopy(ENGLISH_PROFILE), profileId=2, name="Other", tag=None)
    wired(http, profiles=[ENGLISH_PROFILE, other])
    bazarr.wire()
    assert profiles_posted(http) == [(1, "English", ["en", "fr"], "english"), (2, "Other", ["en"], None)]


def test_a_language_bazarr_does_not_know_fails_the_step_after_sonarr_and_radarr_are_connected(bazarr, http, dirs):
    (dirs.config / "apps.yml").write_text("bazarr:\n  languages: [en, xx]\n")
    fresh(http)
    with pytest.raises(bazarr.WiringError, match="bazarr has no language xx"):
        bazarr.wire()
    form = posted(http)
    assert (form["settings-sonarr-ip"], form["settings-radarr-ip"]) == ("sonarr", "radarr")
    assert "languages-profiles" not in form


def test_without_declared_languages_the_subtitle_profiles_are_left_alone(bazarr, http, dirs):
    (dirs.config / "apps.yml").write_text("")
    fresh(http)
    bazarr.wire()
    assert "languages-profiles" not in posted(http)
    assert not [request for request in http.requests if request.path.startswith("/api/system/languages")]


def test_a_dry_run_reports_a_default_profile_that_would_point_elsewhere(wiring, bazarr, http, capsys):
    bazarr = wiring("bazarr", "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n", dry_run=True)
    spanish = dict(copy.deepcopy(ENGLISH_PROFILE), profileId=7, name="Spanish", items=[], tag=None)
    settings = wired_settings()
    settings["general"]["movie_default_profile"] = 7
    wired(http, profiles=[ENGLISH_PROFILE, spanish], settings=settings)
    bazarr.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == "(dry run) bazarr: set the default subtitle profile for movies Spanish -> English\n"
