import os
import stat
import urllib.error

import pytest

from conftest import http_error, refused, reset

SECRETS = "SONARR_API_KEY=sonarr-key\nJELLYFIN_ADMIN_PASSWORD=admin pass\n"


@pytest.fixture
def wirelib(wiring):
    return wiring("wirelib", SECRETS)


@pytest.fixture
def api(wirelib):
    return wirelib.Api("app", "http://app/", {"X-Api-Key": "header-key"})


def test_a_request_is_sent_to_the_apps_address_with_its_headers_and_body(api, http):
    http.on("POST", "/api/thing", {"id": 1})
    assert api.write("POST", "/api/thing", {"name": "x"}) == {"id": 1}
    [request] = http.requests
    assert (request.path, request.body) == ("/api/thing", {"name": "x"})
    assert request.headers["X-api-key"] == "header-key"
    assert request.headers["Content-type"] == "application/json"


def test_a_form_is_sent_url_encoded(api, http):
    http.on("POST", "/api/settings", None)
    api.write("POST", "/api/settings", form={"languages": "en"})
    [request] = http.requests
    assert request.headers["Content-type"] == "application/x-www-form-urlencoded"
    assert request.body == {"languages": "en"}


def test_an_app_that_resets_and_then_answers_503_while_starting_is_asked_again_until_it_answers(api, http):
    http.on("GET", "/System/Info", reset(), http_error(503, "starting"), {"ServerName": "Media"})
    assert api.get("/System/Info") == {"ServerName": "Media"}
    assert len(http.requests) == 3


def test_an_app_that_keeps_answering_503_fails_after_the_attempts_run_out(api, http, monkeypatch):
    monkeypatch.setenv("WIRE_REQUEST_ATTEMPTS", "3")
    http.on("GET", "/System/Info", http_error(503, "starting"))
    with pytest.raises(Exception, match="GET /System/Info answered 503: starting"):
        api.get("/System/Info")
    assert len(http.requests) == 3


@pytest.mark.parametrize("not_processed", [http_error(503, "starting"), refused()], ids=["503", "connection refused"])
def test_a_write_the_app_cannot_have_processed_is_sent_again(api, http, not_processed):
    http.on("POST", "/System/Configuration", not_processed, None)
    api.write("POST", "/System/Configuration", {"ServerName": "Media"})
    assert http.writes() == ["POST /System/Configuration", "POST /System/Configuration"]


@pytest.mark.parametrize(
    "reset_connection",
    [reset(), lambda: urllib.error.URLError(ConnectionResetError("connection reset by peer"))],
    ids=["while waiting for the answer", "while sending, wrapped by urllib"],
)
def test_a_write_whose_connection_is_reset_is_not_sent_again_since_the_app_may_have_acted_on_it(api, http, wirelib, reset_connection):
    http.on("POST", "/System/Configuration", reset_connection, None)
    with pytest.raises(wirelib.WiringError, match="connection reset"):
        api.write("POST", "/System/Configuration", {"ServerName": "Media"})
    assert http.writes() == ["POST /System/Configuration"]


def test_fewer_than_one_attempt_still_sends_the_request_once(api, http, monkeypatch):
    monkeypatch.setenv("WIRE_REQUEST_ATTEMPTS", "0")
    http.on("POST", "/System/Configuration", None)
    api.write("POST", "/System/Configuration", {})
    assert http.writes() == ["POST /System/Configuration"]


def test_an_app_that_rejects_a_request_fails_at_once_with_its_reason(api, http, wirelib):
    http.on("PUT", "/api/v1/settings/jellyfin", http_error(400, '{"message": "request/body/name is read-only"}'))
    with pytest.raises(wirelib.WiringError, match="answered 400: .*name is read-only"):
        api.write("PUT", "/api/v1/settings/jellyfin", {"name": "x"})
    assert len(http.requests) == 1


def test_an_apps_error_that_echoes_a_secret_back_is_reported_with_the_secret_hidden(api, http, wirelib, capsys):
    http.on("POST", "/api/connect", http_error(400, "sonarr at sonarr-key refused, sent with header-key and body-token"))

    def connect():
        api.write("POST", "/api/connect", {"apiKey": "body-token"})

    with pytest.raises(SystemExit) as stopped:
        wirelib.run("app", connect)
    assert stopped.value.code == 1
    reported = capsys.readouterr().err
    assert "refused" in reported
    for secret in ("sonarr-key", "header-key", "body-token"):
        assert secret not in reported


def test_a_dry_run_sends_no_write_and_remembers_nothing(wiring, http, dirs):
    wirelib = wiring("wirelib", SECRETS, dry_run=True)
    api = wirelib.Api("app", "http://app", {})
    assert api.write("POST", "/api/thing", {"name": "x"}) == {"name": "x"}
    wirelib.remember("app.key", "new-key")
    assert http.requests == []
    assert not (dirs.data / "volumes" / ".wiring" / "app.key").exists()


def test_a_dry_run_marks_what_it_reports(wiring, capsys):
    wiring("wirelib", SECRETS, dry_run=True).report("app", "add library Shows")
    assert capsys.readouterr().out == "(dry run) app: add library Shows\n"


def test_what_the_wiring_remembers_is_readable_only_by_the_owner(wirelib, dirs):
    assert wirelib.remembered("app.key") is None
    wirelib.remember("app.key", "new-key")
    assert wirelib.remembered("app.key") == "new-key"
    mode = stat.S_IMODE(os.stat(dirs.data / "volumes" / ".wiring" / "app.key").st_mode)
    assert mode == 0o600


def test_a_request_that_times_out_fails_at_once_without_waiting_for_more_attempts(api, http, wirelib):
    http.on("GET", "/System/Info", lambda: TimeoutError("timed out"))
    with pytest.raises(wirelib.WiringError, match="GET /System/Info failed: timed out"):
        api.get("/System/Info")
    assert len(http.requests) == 1
