import pytest

OK = {"status": "OK", "code": 1, "message": "Success"}
SECRETS = "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n"
WIRED_JELLYFIN = {"jellyfin_url": "http://jellyfin:8096", "jellyfin_api_key": "jellyfin-key", "jellyfin_user_id": "u1"}
WIRED_SEERR = {"url": "http://seerr:5055", "api_key": "seerr-key"}
WIRED_SONARR = {"id": 1, "serverName": "Sonarr", "url": "http://sonarr:8989", "apiKey": "sonarr-key"}
WIRED_RADARR = {"id": 1, "serverName": "Radarr", "url": "http://radarr:7878", "apiKey": "radarr-key"}


@pytest.fixture
def setup_maintainerr(wiring, dirs, monkeypatch):
    seerr = dirs.data / "volumes" / "seerr" / "config"
    seerr.mkdir(parents=True)
    (seerr / "settings.json").write_text('{"main": {"apiKey": "seerr-key"}}')
    store_jellyfin_key(dirs, "jellyfin-key")
    monkeypatch.setenv("MAINTAINERR_URL", "http://maintainerr")

    def load(secrets=SECRETS, dry_run=False):
        return wiring("maintainerr", secrets, dry_run=dry_run)

    return load


@pytest.fixture
def maintainerr(setup_maintainerr):
    return setup_maintainerr()


def store_jellyfin_key(dirs, key):
    wiring_state = dirs.data / "volumes" / ".wiring"
    wiring_state.mkdir(parents=True, exist_ok=True)
    (wiring_state / "jellyfin.key").write_text(key + "\n")


def answering(http, jellyfin, seerr, sonarr, radarr):
    http.on("GET", "/api/settings/jellyfin", jellyfin)
    http.on("GET", "/api/settings/seerr", seerr)
    http.on("GET", "/api/settings/sonarr", sonarr)
    http.on("GET", "/api/settings/radarr", radarr)
    for path in ("/api/settings/jellyfin", "/api/settings/seerr"):
        http.on("POST", path, OK)
    for path in ("/api/settings/sonarr", "/api/settings/radarr"):
        http.on("POST", path, None)
    for path in ("/api/settings/sonarr/1", "/api/settings/radarr/1"):
        http.on("PUT", path, None)


def fresh(http):
    answering(http, {"jellyfin_url": None, "jellyfin_api_key": None}, {"url": None, "api_key": None}, [], [])


def wired(http):
    answering(http, WIRED_JELLYFIN, WIRED_SEERR, [WIRED_SONARR], [WIRED_RADARR])


def test_a_fresh_maintainerr_is_connected_to_jellyfin_seerr_sonarr_and_radarr(maintainerr, http, capsys):
    fresh(http)
    maintainerr.wire()
    assert http.writes() == ["POST /api/settings/jellyfin", "POST /api/settings/seerr", "POST /api/settings/sonarr", "POST /api/settings/radarr"]
    assert http.body("POST", "/api/settings/jellyfin") == {"jellyfin_url": "http://jellyfin:8096", "jellyfin_api_key": "jellyfin-key"}
    assert http.body("POST", "/api/settings/seerr") == {"url": "http://seerr:5055", "api_key": "seerr-key"}
    assert http.body("POST", "/api/settings/sonarr") == {"serverName": "Sonarr", "url": "http://sonarr:8989", "apiKey": "sonarr-key"}
    reported = capsys.readouterr().out
    assert "maintainerr: connect jellyfin" in reported
    assert "sonarr-key" not in reported


def test_an_already_wired_maintainerr_is_left_untouched(maintainerr, http, capsys):
    wired(http)
    maintainerr.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_a_rotated_radarr_key_is_corrected_with_one_write(setup_maintainerr, http):
    maintainerr = setup_maintainerr(SECRETS.replace("RADARR_API_KEY=radarr-key", "RADARR_API_KEY=rotated"))
    wired(http)
    maintainerr.wire()
    assert http.writes() == ["PUT /api/settings/radarr/1"]
    assert http.body("PUT", "/api/settings/radarr/1") == {"serverName": "Radarr", "url": "http://radarr:7878", "apiKey": "rotated"}


def test_an_arr_maintainerr_knows_under_another_name_is_found_by_its_address_and_renamed(maintainerr, http):
    wired(http)
    http.on("GET", "/api/settings/sonarr", [dict(WIRED_SONARR, serverName="TV")])
    maintainerr.wire()
    assert http.writes() == ["PUT /api/settings/sonarr/1"]
    assert http.body("PUT", "/api/settings/sonarr/1")["serverName"] == "Sonarr"


def test_a_new_jellyfin_key_is_sent_again(maintainerr, http, dirs):
    store_jellyfin_key(dirs, "newer-key")
    wired(http)
    maintainerr.wire()
    assert http.writes() == ["POST /api/settings/jellyfin"]
    assert http.body("POST", "/api/settings/jellyfin")["jellyfin_api_key"] == "newer-key"


def test_a_dry_run_reports_the_changes_and_writes_nothing(setup_maintainerr, http, capsys):
    maintainerr = setup_maintainerr(dry_run=True)
    fresh(http)
    maintainerr.wire()
    assert http.writes() == []
    assert "(dry run) maintainerr: connect radarr" in capsys.readouterr().out


def test_without_the_stored_jellyfin_key_the_other_connections_are_still_made(maintainerr, http, dirs):
    (dirs.data / "volumes" / ".wiring" / "jellyfin.key").unlink()
    fresh(http)
    with pytest.raises(maintainerr.WiringError, match="no Jellyfin key in data/volumes/.wiring/jellyfin.key"):
        maintainerr.wire()
    assert http.writes() == ["POST /api/settings/seerr", "POST /api/settings/sonarr", "POST /api/settings/radarr"]


def test_a_setting_maintainerr_answers_as_not_ok_fails_the_step_after_the_others_are_made(maintainerr, http):
    fresh(http)
    http.on("POST", "/api/settings/seerr", {"status": "NOK", "code": 0, "message": "Seerr did not answer"})
    with pytest.raises(maintainerr.WiringError, match="POST /api/settings/seerr: Seerr did not answer"):
        maintainerr.wire()
    assert "POST /api/settings/radarr" in http.writes()


def test_a_dry_run_before_jellyfins_key_exists_still_checks_the_other_connections(setup_maintainerr, http, dirs, capsys):
    maintainerr = setup_maintainerr(dry_run=True)
    (dirs.data / "volumes" / ".wiring" / "jellyfin.key").unlink()
    fresh(http)
    maintainerr.wire()
    assert http.writes() == []
    reported = capsys.readouterr().out
    assert "(dry run) maintainerr: connect jellyfin once the jellyfin wiring has stored its key" in reported
    assert "(dry run) maintainerr: connect sonarr" in reported


def test_a_seerr_that_has_not_written_its_settings_yet_fails_that_connection_only(maintainerr, http, dirs):
    (dirs.data / "volumes" / "seerr" / "config" / "settings.json").unlink()
    fresh(http)
    with pytest.raises(maintainerr.WiringError, match="no Seerr API key in .*settings.json"):
        maintainerr.wire()
    assert http.writes() == ["POST /api/settings/jellyfin", "POST /api/settings/sonarr", "POST /api/settings/radarr"]
