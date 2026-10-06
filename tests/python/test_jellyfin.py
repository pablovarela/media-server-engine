import copy
import shutil

import pytest

from conftest import REPO, http_error

CONFIGURATION = {"ServerName": "Media", "EnableMetrics": False, "UICulture": "en-US"}
LIBRARIES = [
    {"Name": "Collections", "Locations": [], "CollectionType": "boxsets", "ItemId": "c1", "LibraryOptions": {"EnableRealtimeMonitor": False, "Enabled": True}},
    {"Name": "Movies", "Locations": ["/data/media/movies"], "CollectionType": "movies", "ItemId": "m1", "LibraryOptions": {"EnableRealtimeMonitor": True, "Enabled": True}},
    {"Name": "Shows", "Locations": ["/data/media/tvshows"], "CollectionType": "tvshows", "ItemId": "s1", "LibraryOptions": {"EnableRealtimeMonitor": True, "Enabled": True}},
]
STORED_KEY = {"AppName": "media-server", "AccessToken": "stored-key"}


@pytest.fixture
def jellyfin(wiring, dirs, monkeypatch):
    shutil.copy(REPO / "config-template" / "apps.yml", dirs.config / "apps.yml")
    monkeypatch.setenv("JELLYFIN_ADMIN_USER", "admin")
    monkeypatch.setenv("JELLYFIN_URL", "http://jellyfin")
    return wiring("jellyfin", "JELLYFIN_ADMIN_PASSWORD=admin pass\n")


@pytest.fixture
def stored_key(dirs):
    wiring_state = dirs.data / "volumes" / ".wiring"
    wiring_state.mkdir(parents=True)
    (wiring_state / "jellyfin.key").write_text("stored-key\n")
    return wiring_state / "jellyfin.key"


def wired(http, configuration=None, libraries=None, keys=(STORED_KEY,)):
    http.on("GET", "/System/Info/Public", {"StartupWizardCompleted": True})
    http.on("GET", "/System/Info", {"ServerName": "Media"})
    http.on("GET", "/System/Configuration", configuration or dict(CONFIGURATION))
    http.on("GET", "/Library/VirtualFolders", libraries if libraries is not None else copy.deepcopy(LIBRARIES))
    http.on("GET", "/Auth/Keys", {"Items": list(keys)})
    http.on("POST", "/Users/AuthenticateByName", {"AccessToken": "session-token"})
    for path in ("/System/Configuration", "/Library/VirtualFolders/Paths?refreshLibrary=false", "/Library/VirtualFolders/LibraryOptions"):
        http.on("POST", path, None)


def libraries_with(name, **changes):
    libraries = copy.deepcopy(LIBRARIES)
    library = next(library for library in libraries if library["Name"] == name)
    library.update(changes)
    return libraries


def test_a_fresh_jellyfin_completes_the_wizard_then_gets_its_server_name_libraries_and_an_api_key(jellyfin, http, dirs, capsys):
    http.on("GET", "/System/Info/Public", {"StartupWizardCompleted": False})
    http.on("GET", "/Startup/Configuration", {"ServerName": "", "UICulture": "en-US"})
    http.on("GET", "/Startup/User", {"Name": "abc"})
    http.on("POST", "/Users/AuthenticateByName", {"AccessToken": "session-token"})
    http.on("GET", "/Auth/Keys", {"Items": []}, {"Items": [{"AppName": "media-server", "AccessToken": "new-key"}]})
    http.on("GET", "/System/Configuration", {"ServerName": "", "EnableMetrics": False})
    http.on("GET", "/Library/VirtualFolders", [])
    for path in ("/Startup/Configuration", "/Startup/User", "/Startup/RemoteAccess", "/Startup/Complete", "/Auth/Keys?app=media-server", "/System/Configuration", "/Library/Refresh"):
        http.on("POST", path, None)
    for query in ("name=Shows&collectionType=tvshows&paths=%2Fdata%2Fmedia%2Ftvshows", "name=Movies&collectionType=movies&paths=%2Fdata%2Fmedia%2Fmovies"):
        http.on("POST", f"/Library/VirtualFolders?{query}&refreshLibrary=false", None)

    jellyfin.wire()

    assert http.writes() == [
        "POST /Startup/Configuration",
        "POST /Startup/User",
        "POST /Startup/RemoteAccess",
        "POST /Startup/Complete",
        "POST /Users/AuthenticateByName",
        "POST /Auth/Keys?app=media-server",
        "POST /System/Configuration",
        "POST /Library/VirtualFolders?name=Shows&collectionType=tvshows&paths=%2Fdata%2Fmedia%2Ftvshows&refreshLibrary=false",
        "POST /Library/VirtualFolders?name=Movies&collectionType=movies&paths=%2Fdata%2Fmedia%2Fmovies&refreshLibrary=false",
        "POST /Library/Refresh",
    ]
    assert http.body("POST", "/Startup/Configuration")["ServerName"] == "Media"
    assert http.body("POST", "/Startup/User") == {"Name": "admin", "Password": "admin pass"}
    assert http.body("POST", "/Users/AuthenticateByName") == {"Username": "admin", "Pw": "admin pass"}
    assert http.body("POST", "/Library/VirtualFolders?name=Shows&collectionType=tvshows&paths=%2Fdata%2Fmedia%2Ftvshows&refreshLibrary=false") == {"LibraryOptions": {"EnableRealtimeMonitor": True}}
    assert (dirs.data / "volumes" / ".wiring" / "jellyfin.key").read_text() == "new-key\n"
    assert "jellyfin: add library Shows" in capsys.readouterr().out


def test_an_already_wired_jellyfin_is_left_untouched_including_libraries_it_was_not_told_about(jellyfin, http, stored_key, capsys):
    wired(http)
    jellyfin.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_the_wiring_signs_its_requests_with_the_stored_key(jellyfin, http, stored_key):
    wired(http)
    jellyfin.wire()
    libraries_request = next(request for request in http.requests if request.path == "/Library/VirtualFolders")
    assert 'Token="stored-key"' in libraries_request.headers["Authorization"]


def test_a_completed_wizard_is_not_run_again(jellyfin, http):
    wired(http)
    jellyfin.wire()
    assert not [request for request in http.requests if request.path.startswith("/Startup/")]


def test_without_a_stored_key_the_one_jellyfin_has_is_stored_without_creating_another(jellyfin, http, dirs):
    wired(http)
    jellyfin.wire()
    assert "POST /Auth/Keys?app=media-server" not in http.writes()
    assert (dirs.data / "volumes" / ".wiring" / "jellyfin.key").read_text() == "stored-key\n"


def test_a_stored_key_jellyfin_no_longer_accepts_is_replaced_by_the_key_jellyfin_has(jellyfin, http, stored_key):
    wired(http, keys=[{"AppName": "media-server", "AccessToken": "restored-key"}])
    http.on("GET", "/System/Info", http_error(401))
    jellyfin.wire()
    assert "POST /Users/AuthenticateByName" in http.writes()
    assert stored_key.read_text() == "restored-key\n"


def test_a_drifted_server_name_is_corrected_with_one_write_that_keeps_the_other_settings(jellyfin, http, stored_key, capsys):
    wired(http, configuration=dict(CONFIGURATION, ServerName="Other"))
    jellyfin.wire()
    assert http.writes() == ["POST /System/Configuration"]
    assert http.body("POST", "/System/Configuration") == CONFIGURATION
    assert "jellyfin: set server name Other -> Media" in capsys.readouterr().out


def test_a_declared_path_missing_from_a_library_is_added_to_it_and_scanned(jellyfin, http, stored_key):
    wired(http, libraries=libraries_with("Shows", Locations=[]))
    http.on("POST", "/Library/Refresh", None)
    jellyfin.wire()
    assert http.writes() == ["POST /Library/VirtualFolders/Paths?refreshLibrary=false", "POST /Library/Refresh"]
    assert http.body("POST", "/Library/VirtualFolders/Paths?refreshLibrary=false") == {"Name": "Shows", "PathInfo": {"Path": "/data/media/tvshows"}}


def test_a_library_with_real_time_monitoring_off_gets_it_on_keeping_its_other_options(jellyfin, http, stored_key, capsys):
    wired(http, libraries=libraries_with("Shows", LibraryOptions={"EnableRealtimeMonitor": False, "Enabled": True}))
    jellyfin.wire()
    assert http.writes() == ["POST /Library/VirtualFolders/LibraryOptions"]
    assert http.body("POST", "/Library/VirtualFolders/LibraryOptions") == {"Id": "s1", "LibraryOptions": {"EnableRealtimeMonitor": True, "Enabled": True}}
    assert "jellyfin: turn on real-time monitoring for library Shows" in capsys.readouterr().out


def test_a_library_of_another_type_fails_the_step_after_the_rest_is_wired(jellyfin, http, stored_key):
    wired(http, configuration=dict(CONFIGURATION, ServerName="Other"), libraries=libraries_with("Shows", CollectionType="movies"))
    with pytest.raises(Exception, match="library Shows is movies, not tvshows; change it in Jellyfin"):
        jellyfin.wire()
    assert http.writes() == ["POST /System/Configuration"]


def test_a_sign_in_jellyfin_refuses_fails_with_the_admin_user_named(jellyfin, http):
    wired(http)
    http.on("POST", "/Users/AuthenticateByName", http_error(401, "Invalid username or password"))
    with pytest.raises(Exception, match="cannot sign in as admin"):
        jellyfin.wire()


def test_a_dry_run_on_a_fresh_jellyfin_reports_the_wizard_and_libraries_and_writes_nothing(wiring, dirs, http, monkeypatch, capsys):
    shutil.copy(REPO / "config-template" / "apps.yml", dirs.config / "apps.yml")
    monkeypatch.setenv("JELLYFIN_ADMIN_USER", "admin")
    monkeypatch.setenv("JELLYFIN_URL", "http://jellyfin")
    jellyfin = wiring("jellyfin", "JELLYFIN_ADMIN_PASSWORD=admin pass\n", dry_run=True)
    http.on("GET", "/System/Info/Public", {"StartupWizardCompleted": False})
    http.on("GET", "/Startup/Configuration", {"ServerName": ""})
    http.on("GET", "/Startup/User", {"Name": "abc"})
    jellyfin.wire()
    assert http.writes() == []
    reported = capsys.readouterr().out
    assert "(dry run) jellyfin: complete the startup wizard with admin user admin" in reported
    assert "(dry run) jellyfin: add library Shows" in reported
    assert not (dirs.data / "volumes" / ".wiring" / "jellyfin.key").exists()


def test_a_dry_run_on_a_jellyfin_that_lost_its_key_reports_creating_one_and_writes_nothing(wiring, dirs, http, monkeypatch, capsys):
    shutil.copy(REPO / "config-template" / "apps.yml", dirs.config / "apps.yml")
    monkeypatch.setenv("JELLYFIN_ADMIN_USER", "admin")
    monkeypatch.setenv("JELLYFIN_URL", "http://jellyfin")
    jellyfin = wiring("jellyfin", "JELLYFIN_ADMIN_PASSWORD=admin pass\n", dry_run=True)
    wired(http, keys=[])
    jellyfin.wire()
    assert "(dry run) jellyfin: create API key media-server" in capsys.readouterr().out
    assert [write for write in http.writes() if write != "POST /Users/AuthenticateByName"] == []
    assert not (dirs.data / "volumes" / ".wiring" / "jellyfin.key").exists()


def test_without_a_declared_server_name_jellyfins_own_is_left_alone(jellyfin, http, stored_key, dirs):
    (dirs.config / "apps.yml").write_text("jellyfin:\n  libraries: []\n")
    wired(http, configuration=dict(CONFIGURATION, ServerName="Named by hand"))
    jellyfin.wire()
    assert http.writes() == []
