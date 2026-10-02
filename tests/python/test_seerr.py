import copy
import shutil

import pytest
import yaml

from conftest import REPO

SONARR_TEST = {"profiles": [{"id": 4, "name": "Any"}, {"id": 7, "name": "WEB-1080p"}], "rootFolders": [{"id": 1, "path": "/tv"}]}
RADARR_TEST = {"profiles": [{"id": 7, "name": "HD Bluray + WEB"}], "rootFolders": [{"id": 1, "path": "/movies"}]}
LIBRARIES = [
    {"id": "m1", "name": "Movies", "enabled": True, "type": "movie"},
    {"id": "s1", "name": "Shows", "enabled": True, "type": "show"},
    {"id": "c1", "name": "Collections", "enabled": False, "type": "movie"},
]
SONARR = {
    "id": 0, "name": "Sonarr", "hostname": "sonarr", "port": 8989, "apiKey": "sonarr-key", "useSsl": False, "baseUrl": "",
    "activeProfileId": 7, "activeProfileName": "WEB-1080p", "activeDirectory": "/tv",
    "activeAnimeProfileId": 7, "activeAnimeProfileName": "WEB-1080p", "activeAnimeDirectory": "/tv",
    "is4k": False, "isDefault": True, "enableSeasonFolders": False, "syncEnabled": True, "preventSearch": False, "tags": [], "animeTags": [],
}
RADARR = {
    "id": 0, "name": "Radarr", "hostname": "radarr", "port": 7878, "apiKey": "radarr-key", "useSsl": False, "baseUrl": "",
    "activeProfileId": 7, "activeProfileName": "HD Bluray + WEB", "activeDirectory": "/movies",
    "is4k": False, "minimumAvailability": "released", "isDefault": True, "syncEnabled": True, "preventSearch": False, "tags": [],
}
SECRETS = "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\nJELLYFIN_ADMIN_PASSWORD=admin pass\n"


@pytest.fixture
def setup_seerr(wiring, dirs, monkeypatch):
    settings = dirs.data / "volumes" / "seerr" / "config"
    settings.mkdir(parents=True)
    (settings / "settings.json").write_text('{"main": {"apiKey": "seerr-key"}}')
    shutil.copy(REPO / "config-template" / "apps.yml", dirs.config / "apps.yml")
    monkeypatch.setenv("JELLYFIN_ADMIN_USER", "admin")
    monkeypatch.setenv("SEERR_URL", "http://seerr")

    def load(secrets=SECRETS, dry_run=False):
        return wiring("seerr", secrets, dry_run=dry_run)

    return load


@pytest.fixture
def seerr(setup_seerr):
    return setup_seerr()


def seerr_config(dirs, **changes):
    path = dirs.config / "apps.yml"
    config = yaml.safe_load(path.read_text())
    config["seerr"].update(changes)
    path.write_text(yaml.safe_dump(config))


def tests_of_the_arrs(http):
    http.on("POST", "/api/v1/settings/sonarr/test", SONARR_TEST)
    http.on("POST", "/api/v1/settings/radarr/test", RADARR_TEST)


def fresh(http):
    http.on("GET", "/api/v1/settings/public", {"initialized": False, "mediaServerType": 4})
    http.on("POST", "/api/v1/auth/jellyfin", {"id": 1})
    http.on("GET", "/api/v1/settings/jellyfin/library", [])
    http.on("POST", "/api/v1/settings/jellyfin/library/sync", [dict(library, enabled=False) for library in LIBRARIES])
    for library in ("s1", "m1"):
        http.on("PUT", f"/api/v1/settings/jellyfin/library/{library}", None)
    for kind in ("sonarr", "radarr"):
        http.on("GET", f"/api/v1/settings/{kind}", [])
        http.on("POST", f"/api/v1/settings/{kind}", None)
    tests_of_the_arrs(http)
    http.on("POST", "/api/v1/settings/initialize", {"initialized": True})


def wired(http, sonarr=None, radarr=None, libraries=None, jellyfin=None):
    http.on("GET", "/api/v1/settings/public", {"initialized": True, "mediaServerType": 2})
    http.on("GET", "/api/v1/settings/jellyfin/library", libraries or copy.deepcopy(LIBRARIES))
    http.on("GET", "/api/v1/settings/jellyfin", jellyfin or {"externalHostname": ""})
    http.on("GET", "/api/v1/settings/sonarr", [sonarr or dict(SONARR)])
    http.on("GET", "/api/v1/settings/radarr", [radarr or dict(RADARR)])
    tests_of_the_arrs(http)
    for path in ("/api/v1/settings/sonarr/0", "/api/v1/settings/radarr/0", "/api/v1/settings/jellyfin/library/s1"):
        http.on("PUT", path, None)
    http.on("POST", "/api/v1/settings/jellyfin", None)


def test_a_fresh_seerr_signs_in_with_jellyfin_gets_libraries_sonarr_and_radarr_then_is_initialised(seerr, http):
    fresh(http)
    seerr.wire()
    assert http.writes() == [
        "POST /api/v1/auth/jellyfin",
        "POST /api/v1/settings/jellyfin/library/sync",
        "PUT /api/v1/settings/jellyfin/library/s1",
        "PUT /api/v1/settings/jellyfin/library/m1",
        "POST /api/v1/settings/sonarr/test",
        "POST /api/v1/settings/sonarr",
        "POST /api/v1/settings/radarr/test",
        "POST /api/v1/settings/radarr",
        "POST /api/v1/settings/initialize",
    ]
    auth = http.body("POST", "/api/v1/auth/jellyfin")
    assert (auth["username"], auth["password"], auth["hostname"], auth["port"], auth["serverType"]) == ("admin", "admin pass", "jellyfin", 8096, 2)
    sonarr = http.body("POST", "/api/v1/settings/sonarr")
    assert (sonarr["activeProfileId"], sonarr["activeDirectory"], sonarr["apiKey"], sonarr["isDefault"]) == (7, "/tv", "sonarr-key", True)
    assert (sonarr["activeAnimeProfileId"], sonarr["activeAnimeDirectory"], sonarr["enableSeasonFolders"]) == (7, "/tv", False)
    radarr = http.body("POST", "/api/v1/settings/radarr")
    assert (radarr["activeProfileId"], radarr["activeDirectory"], radarr["minimumAvailability"]) == (7, "/movies", "released")
    assert http.body("PUT", "/api/v1/settings/jellyfin/library/s1") == {"enabled": True}


def test_the_wiring_signs_its_requests_with_seerrs_own_key(seerr, http):
    wired(http)
    seerr.wire()
    assert {request.headers["X-api-key"] for request in http.requests} == {"seerr-key"}


def test_an_already_wired_seerr_is_left_untouched(seerr, http, capsys):
    wired(http)
    seerr.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_a_rotated_sonarr_key_is_corrected_with_one_write_that_keeps_the_other_settings(setup_seerr, http, capsys):
    seerr = setup_seerr(SECRETS.replace("SONARR_API_KEY=sonarr-key", "SONARR_API_KEY=rotated"))
    wired(http)
    seerr.wire()
    assert http.writes() == ["PUT /api/v1/settings/sonarr/0"]
    expected = {field: value for field, value in SONARR.items() if field != "id"}
    assert http.body("PUT", "/api/v1/settings/sonarr/0") == dict(expected, apiKey="rotated")
    assert "seerr: set sonarr api key" in capsys.readouterr().out


def test_a_changed_quality_profile_is_looked_up_by_name(seerr, http, capsys):
    wired(http, sonarr=dict(SONARR, activeProfileId=4, activeProfileName="Any"))
    seerr.wire()
    assert http.writes() == ["POST /api/v1/settings/sonarr/test", "PUT /api/v1/settings/sonarr/0"]
    body = http.body("PUT", "/api/v1/settings/sonarr/0")
    assert (body["activeProfileId"], body["activeProfileName"], body["activeAnimeProfileId"]) == (7, "WEB-1080p", 7)
    assert "seerr: set sonarr quality profile Any -> WEB-1080p" in capsys.readouterr().out


def test_a_changed_minimum_availability_for_radarr_is_corrected(seerr, http, capsys):
    wired(http, radarr=dict(RADARR, minimumAvailability="announced"))
    seerr.wire()
    assert http.writes() == ["PUT /api/v1/settings/radarr/0"]
    assert http.body("PUT", "/api/v1/settings/radarr/0")["minimumAvailability"] == "released"
    assert "seerr: set radarr minimum availability announced -> released" in capsys.readouterr().out


def test_a_declared_library_that_is_not_enabled_is_enabled_without_a_sync(seerr, http):
    libraries = copy.deepcopy(LIBRARIES)
    libraries[1]["enabled"] = False
    wired(http, libraries=libraries)
    seerr.wire()
    assert http.writes() == ["PUT /api/v1/settings/jellyfin/library/s1"]


def test_a_quality_profile_an_app_does_not_have_fails_the_step_after_the_other_app_is_wired_and_leaves_seerr_to_finish_next_time(seerr, http, dirs):
    config = yaml.safe_load((dirs.config / "apps.yml").read_text())
    config["seerr"]["sonarr"]["quality_profile"] = "Missing"
    (dirs.config / "apps.yml").write_text(yaml.safe_dump(config))
    fresh(http)
    with pytest.raises(seerr.WiringError, match="sonarr has no quality profile Missing"):
        seerr.wire()
    assert "POST /api/v1/settings/radarr" in http.writes()
    assert "POST /api/v1/settings/sonarr" not in http.writes()
    assert "POST /api/v1/settings/initialize" not in http.writes()


def test_a_declared_library_jellyfin_does_not_have_fails_the_step(seerr, http, dirs):
    seerr_config(dirs, libraries=["Shows", "Cartoons"])
    fresh(http)
    with pytest.raises(seerr.WiringError, match="jellyfin has no library Cartoons"):
        seerr.wire()


def test_a_dry_run_on_a_fresh_seerr_reports_its_setup_and_writes_nothing(setup_seerr, http, capsys):
    seerr = setup_seerr(dry_run=True)
    http.on("GET", "/api/v1/settings/public", {"initialized": False, "mediaServerType": 4})
    seerr.wire()
    assert http.writes() == []
    assert capsys.readouterr().out.splitlines() == [
        "(dry run) seerr: sign in with jellyfin as admin",
        "(dry run) seerr: enable library Shows",
        "(dry run) seerr: enable library Movies",
        "(dry run) seerr: add sonarr with quality profile WEB-1080p and root folder /tv",
        "(dry run) seerr: add radarr with quality profile HD Bluray + WEB and root folder /movies",
        "(dry run) seerr: initialise",
    ]


def test_a_seerr_that_has_not_written_its_settings_yet_fails_with_a_hint(seerr, http, dirs):
    (dirs.data / "volumes" / "seerr" / "config" / "settings.json").unlink()
    with pytest.raises(seerr.WiringError, match="no API key in .*settings.json; start seerr once"):
        seerr.wire()
    assert http.requests == []


def test_a_declared_jellyfin_external_url_is_set_with_one_write_of_that_setting_alone(seerr, http, dirs, capsys):
    seerr_config(dirs, jellyfin_external_url="http://gorgon.local:8096")
    wired(http, jellyfin={"name": "Media", "externalHostname": "http://media.local:8096"})
    seerr.wire()
    assert http.writes() == ["POST /api/v1/settings/jellyfin"]
    assert http.body("POST", "/api/v1/settings/jellyfin") == {"externalHostname": "http://gorgon.local:8096"}
    assert "seerr: set jellyfin external url http://media.local:8096 -> http://gorgon.local:8096" in capsys.readouterr().out


def test_an_external_url_seerr_already_has_is_not_written_again(seerr, http, dirs):
    seerr_config(dirs, jellyfin_external_url="http://gorgon.local:8096")
    wired(http, jellyfin={"externalHostname": "http://gorgon.local:8096"})
    seerr.wire()
    assert http.writes() == []


def test_a_dry_run_reports_a_library_and_an_app_seerr_does_not_have_yet_without_failing(setup_seerr, http, dirs, capsys):
    seerr = setup_seerr(dry_run=True)
    seerr_config(dirs, libraries=["Shows", "Movies", "Cartoons"])
    wired(http)
    http.on("GET", "/api/v1/settings/radarr", [])
    seerr.wire()
    assert http.writes() == []
    reported = capsys.readouterr().out.splitlines()
    assert [line for line in reported if "enable library" in line] == ["(dry run) seerr: enable library Cartoons"]
    assert "(dry run) seerr: add radarr with quality profile HD Bluray + WEB and root folder /movies" in reported


def test_a_root_folder_an_app_does_not_have_fails_the_step_with_the_folder_named(seerr, http, dirs):
    config = yaml.safe_load((dirs.config / "apps.yml").read_text())
    config["seerr"]["radarr"]["root_folder"] = "/films"
    (dirs.config / "apps.yml").write_text(yaml.safe_dump(config))
    wired(http)
    with pytest.raises(seerr.WiringError, match="radarr has no root folder /films"):
        seerr.wire()


def test_a_port_changed_by_hand_in_seerr_is_put_back(seerr, http, capsys):
    wired(http, sonarr=dict(SONARR, port=9999))
    seerr.wire()
    assert http.writes() == ["PUT /api/v1/settings/sonarr/0"]
    assert http.body("PUT", "/api/v1/settings/sonarr/0")["port"] == 8989
    assert "seerr: set sonarr port 9999 -> 8989" in capsys.readouterr().out


def test_a_changed_root_folder_is_applied_to_series_and_anime(seerr, http, dirs, capsys):
    config = yaml.safe_load((dirs.config / "apps.yml").read_text())
    config["seerr"]["sonarr"]["root_folder"] = "/series"
    (dirs.config / "apps.yml").write_text(yaml.safe_dump(config))
    wired(http)
    http.on("POST", "/api/v1/settings/sonarr/test", dict(SONARR_TEST, rootFolders=[{"id": 1, "path": "/tv"}, {"id": 2, "path": "/series"}]))
    seerr.wire()
    body = http.body("PUT", "/api/v1/settings/sonarr/0")
    assert (body["activeDirectory"], body["activeAnimeDirectory"]) == ("/series", "/series")
    assert "seerr: set sonarr root folder /tv -> /series" in capsys.readouterr().out
