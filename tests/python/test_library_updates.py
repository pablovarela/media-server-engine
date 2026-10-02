import copy
import hashlib

import pytest

SONARR_EVENTS = ["onApplicationUpdate", "onDownload", "onEpisodeFileDelete", "onEpisodeFileDeleteForUpgrade", "onGrab", "onHealthIssue", "onHealthRestored",
                 "onImportComplete", "onManualInteractionRequired", "onRename", "onSeriesAdd", "onSeriesDelete", "onUpgrade"]
RADARR_EVENTS = ["onApplicationUpdate", "onDownload", "onGrab", "onHealthIssue", "onHealthRestored", "onManualInteractionRequired", "onMovieAdded",
                 "onMovieDelete", "onMovieFileDelete", "onMovieFileDeleteForUpgrade", "onRename", "onUpgrade"]
LEFT_OFF = {"onHealthIssue", "onHealthRestored", "onManualInteractionRequired"}
PATHS = {"sonarr": ("/tv", "/data/tvshows"), "radarr": ("/movies", "/data/movies")}


def schema(events):
    fields = [("host", None), ("port", 8096), ("useSsl", False), ("urlBase", None), ("apiKey", None), ("notify", False), ("updateLibrary", True), ("mapFrom", None), ("mapTo", None)]
    return dict({event: False for event in events}, name="", implementation="MediaBrowser", tags=[], fields=[{"name": n, "value": v} for n, v in fields])


def connection(kind, events):
    item = schema(events)
    item.update({event: event not in LEFT_OFF for event in events}, id=2, name="Emby / Jellyfin")
    map_from, map_to = PATHS[kind]
    values = {"host": "jellyfin", "apiKey": "********", "mapFrom": map_from, "mapTo": map_to}
    for field in item["fields"]:
        field["value"] = values.get(field["name"], field["value"])
    return item


def fields_of(body):
    return {field["name"]: field["value"] for field in body["fields"]}


@pytest.fixture
def setup_library_updates(wiring, dirs, monkeypatch):
    store_jellyfin_key(dirs, "jellyfin-key")
    monkeypatch.setenv("SONARR_URL", "http://sonarr")
    monkeypatch.setenv("RADARR_URL", "http://radarr")

    def load(dry_run=False):
        return wiring("library_updates", "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\n", dry_run=dry_run)

    return load


@pytest.fixture
def library_updates(setup_library_updates):
    return setup_library_updates()


def store_jellyfin_key(dirs, key):
    wiring_state = dirs.data / "volumes" / ".wiring"
    wiring_state.mkdir(parents=True, exist_ok=True)
    (wiring_state / "jellyfin.key").write_text(key + "\n")


def remember_applied_key(dirs, key):
    for kind in ("sonarr", "radarr"):
        (dirs.data / "volumes" / ".wiring" / f"{kind}-jellyfin-connection.sha256").write_text(hashlib.sha256(key.encode()).hexdigest() + "\n")


def answering(http, sonarr_connections, radarr_connections):
    for host, events, connections in (("http://sonarr", SONARR_EVENTS, sonarr_connections), ("http://radarr", RADARR_EVENTS, radarr_connections)):
        http.on("GET", f"{host}/api/v3/notification", connections)
        http.on("GET", f"{host}/api/v3/notification/schema", [schema(events)])
        http.on("POST", f"{host}/api/v3/notification?forceSave=true", None)
        http.on("PUT", f"{host}/api/v3/notification/2?forceSave=true", None)


def fresh(http):
    answering(http, [], [])


def wired(http, sonarr=None):
    answering(http, [sonarr or connection("sonarr", SONARR_EVENTS)], [connection("radarr", RADARR_EVENTS)])


def test_fresh_sonarr_and_radarr_tell_jellyfin_about_every_library_change(library_updates, http, capsys):
    fresh(http)
    library_updates.wire()
    assert http.writes("sonarr") == ["POST /api/v3/notification?forceSave=true"]
    assert http.writes("radarr") == ["POST /api/v3/notification?forceSave=true"]
    sonarr = http.body("POST", "/api/v3/notification?forceSave=true", "sonarr")
    assert sonarr["name"] == "Emby / Jellyfin"
    assert fields_of(sonarr) == {"host": "jellyfin", "port": 8096, "useSsl": False, "urlBase": None, "apiKey": "jellyfin-key", "notify": False, "updateLibrary": True, "mapFrom": "/tv", "mapTo": "/data/tvshows"}
    assert sorted(event for event in SONARR_EVENTS if sonarr[event]) == sorted(set(SONARR_EVENTS) - LEFT_OFF)
    radarr = http.body("POST", "/api/v3/notification?forceSave=true", "radarr")
    assert (fields_of(radarr)["mapFrom"], fields_of(radarr)["mapTo"], radarr["onMovieDelete"]) == ("/movies", "/data/movies", True)
    assert "sonarr: add the Jellyfin connection" in capsys.readouterr().out


def test_each_arr_is_asked_with_its_own_key(library_updates, http):
    fresh(http)
    library_updates.wire()
    assert {(request.host, request.headers["X-api-key"]) for request in http.requests} == {("sonarr", "sonarr-key"), ("radarr", "radarr-key")}


def test_already_connected_arrs_are_left_untouched(library_updates, http, dirs, capsys):
    remember_applied_key(dirs, "jellyfin-key")
    wired(http)
    library_updates.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_a_new_jellyfin_key_reaches_both_connections_once(setup_library_updates, http, dirs):
    remember_applied_key(dirs, "old-key")
    wired(http)
    setup_library_updates().wire()
    assert http.writes() == ["PUT /api/v3/notification/2?forceSave=true", "PUT /api/v3/notification/2?forceSave=true"]
    assert fields_of(http.body("PUT", "/api/v3/notification/2?forceSave=true", "sonarr"))["apiKey"] == "jellyfin-key"
    http.requests.clear()
    setup_library_updates().wire()
    assert http.writes() == []


def test_a_connection_pointed_elsewhere_is_corrected_keeping_its_events_and_the_real_key(library_updates, http, dirs, capsys):
    remember_applied_key(dirs, "jellyfin-key")
    moved = connection("sonarr", SONARR_EVENTS)
    moved["onGrab"] = False
    next(field for field in moved["fields"] if field["name"] == "host")["value"] = "old-host"
    wired(http, sonarr=copy.deepcopy(moved))
    library_updates.wire()
    assert http.writes() == ["PUT /api/v3/notification/2?forceSave=true"]
    body = http.body("PUT", "/api/v3/notification/2?forceSave=true", "sonarr")
    assert (fields_of(body)["host"], fields_of(body)["apiKey"], body["onGrab"]) == ("jellyfin", "jellyfin-key", False)
    assert "sonarr: set the Jellyfin connection host old-host -> jellyfin" in capsys.readouterr().out


def test_a_connection_without_the_path_mapping_gets_it_since_jellyfin_cannot_see_the_arrs_paths(library_updates, http, dirs):
    remember_applied_key(dirs, "jellyfin-key")
    unmapped = connection("sonarr", SONARR_EVENTS)
    for field in unmapped["fields"]:
        if field["name"] in ("mapFrom", "mapTo"):
            field["value"] = None
    wired(http, sonarr=unmapped)
    library_updates.wire()
    assert http.writes("sonarr") == ["PUT /api/v3/notification/2?forceSave=true"]
    assert http.writes("radarr") == []
    body = fields_of(http.body("PUT", "/api/v3/notification/2?forceSave=true", "sonarr"))
    assert (body["mapFrom"], body["mapTo"]) == ("/tv", "/data/tvshows")


def test_a_dry_run_reports_and_writes_nothing_or_remembers_any_key(setup_library_updates, http, dirs, capsys):
    library_updates = setup_library_updates(dry_run=True)
    fresh(http)
    library_updates.wire()
    assert http.writes() == []
    assert "(dry run) radarr: add the Jellyfin connection" in capsys.readouterr().out
    assert not (dirs.data / "volumes" / ".wiring" / "radarr-jellyfin-connection.sha256").exists()


def test_without_the_stored_jellyfin_key_the_step_fails_with_a_hint(library_updates, http, dirs):
    (dirs.data / "volumes" / ".wiring" / "jellyfin.key").unlink()
    with pytest.raises(library_updates.WiringError, match="no Jellyfin key in data/volumes/.wiring/jellyfin.key"):
        library_updates.wire()
    assert http.requests == []


def test_a_dry_run_before_jellyfins_key_exists_says_what_will_happen_instead_of_failing(setup_library_updates, http, dirs, capsys):
    library_updates = setup_library_updates(dry_run=True)
    (dirs.data / "volumes" / ".wiring" / "jellyfin.key").unlink()
    library_updates.wire()
    assert http.requests == []
    assert "(dry run) library-updates: connect Sonarr and Radarr to Jellyfin once the jellyfin wiring has stored its key" in capsys.readouterr().out


def test_an_arr_that_fails_does_not_stop_the_other_being_connected(library_updates, http):
    fresh(http)
    http.on("GET", "http://sonarr/api/v3/notification", lambda: OSError("connection refused by firewall"))
    with pytest.raises(library_updates.WiringError, match="sonarr: GET /api/v3/notification failed"):
        library_updates.wire()
    assert http.writes("radarr") == ["POST /api/v3/notification?forceSave=true"]
