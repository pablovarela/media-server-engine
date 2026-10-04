import os

import pytest

from conftest import fresh_engine, refused

EXE = {"messages": ["Caution: Found executable file with extension: '.exe'"]}


def flagged(record_id, title, download_id=None):
    record = {"id": record_id, "title": title, "statusMessages": [EXE]}
    if download_id:
        record["downloadId"] = download_id
    return record


def queue(port, page=1, size=200):
    return f"http://localhost:{port}/api/v3/queue?page={page}&pageSize={size}"


def removal(port, record_id):
    return f"http://localhost:{port}/api/v3/queue/{record_id}?removeFromClient=true&blocklist=true&skipRedownload=false"


@pytest.fixture
def cleanup(installed, http):
    sonarr, radarr = installed.data / "volumes" / "sonarr" / "data", installed.data / "volumes" / "radarr" / "config"
    sonarr.mkdir(parents=True)
    radarr.mkdir(parents=True)
    (sonarr / "config.xml").write_text("<Config><ApiKey>sonarr-key</ApiKey></Config>\n")
    (radarr / "config.xml").write_text("<Config>\n  <ApiKey>radarr-key</ApiKey>\n</Config>\n")
    http.on("GET", queue(8989), {"records": [flagged(11, "Show S01E01.exe"), {"id": 12, "title": "Show S01E02", "statusMessages": []}]})
    http.on("GET", queue(7878), {"records": []})
    http.on("DELETE", removal(8989, 11), None)
    return fresh_engine("engine.downloads")


def test_cleanup_removes_and_blocklists_downloads_flagged_as_executables(cleanup, http, capsys):
    assert cleanup.main([]) == 0
    assert http.writes() == ["DELETE /api/v3/queue/11?removeFromClient=true&blocklist=true&skipRedownload=false"]
    assert capsys.readouterr().out == "sonarr: removed and blocklisted Show S01E01.exe\n"


def test_cleanup_asks_each_app_with_its_own_key(cleanup, http):
    cleanup.main([])
    keys = {r.host: r.headers.get("X-api-key") for r in http.requests}
    assert keys == {"localhost:8989": "sonarr-key", "localhost:7878": "radarr-key"}


def test_cleanup_still_checks_sonarr_when_radarr_is_unreachable(cleanup, http, capsys):
    http.on("GET", queue(7878), refused())
    assert cleanup.main([]) == 0
    assert "DELETE /api/v3/queue/11?removeFromClient=true&blocklist=true&skipRedownload=false" in http.writes()
    assert capsys.readouterr().out.endswith("radarr: queue not reachable, skipped\n")


def test_an_app_without_its_config_yet_is_skipped(cleanup, installed, capsys):
    os.remove(installed.data / "volumes" / "radarr" / "config" / "config.xml")
    cleanup.main([])
    assert "radarr: queue not reachable, skipped\n" in capsys.readouterr().out


def test_cleanup_removes_a_flagged_season_pack_once_not_once_per_episode(cleanup, http):
    http.on("GET", queue(8989), {"totalRecords": 2, "records": [flagged(21, "Show S02.exe", "PACK"), flagged(22, "Show S02.exe", "PACK")]})
    http.on("DELETE", removal(8989, 21), None)
    cleanup.main([])
    assert http.writes() == ["DELETE /api/v3/queue/21?removeFromClient=true&blocklist=true&skipRedownload=false"]


def test_cleanup_reads_every_page_of_the_queue(cleanup, http, monkeypatch):
    monkeypatch.setenv("QUEUE_PAGE_SIZE", "1")
    http.on("GET", queue(8989, 1, 1), {"totalRecords": 2, "records": [flagged(31, "One.exe", "A")]})
    http.on("GET", queue(8989, 2, 1), {"totalRecords": 2, "records": [flagged(32, "Two.exe", "B")]})
    http.on("GET", queue(7878, 1, 1), {"records": []})
    http.on("DELETE", removal(8989, 31), None)
    http.on("DELETE", removal(8989, 32), None)
    cleanup.main([])
    assert [w.split("?")[0] for w in http.writes()] == ["DELETE /api/v3/queue/31", "DELETE /api/v3/queue/32"]


def test_a_removal_that_fails_is_reported_and_the_others_carry_on(cleanup, http, capsys):
    http.on("GET", queue(8989), {"records": [flagged(41, "A.exe", "A"), flagged(42, "B.exe", "B")]})
    http.on("DELETE", removal(8989, 41), refused())
    http.on("DELETE", removal(8989, 42), None)
    assert cleanup.main([]) == 0
    output = capsys.readouterr()
    assert output.out == "sonarr: removed and blocklisted B.exe\n"
    assert output.err.startswith("sonarr: could not remove A.exe: ")
