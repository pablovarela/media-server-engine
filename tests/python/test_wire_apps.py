import subprocess

import pytest

from conftest import http_error, refused


@pytest.fixture
def wire_apps(wiring, monkeypatch):
    monkeypatch.setenv("WIRE_RETRY_SECONDS", "0")
    module = wiring("wire_apps")
    monkeypatch.setattr(module.time, "sleep", lambda seconds: None)
    return module


def recording(ran, name, error=None):
    def step():
        ran.append(name)
        if error is not None:
            raise error

    return step


def test_the_engine_wires_the_apps_in_dependency_order(wire_apps):
    assert [name for name, _, _ in wire_apps.STEPS] == [
        "prowlarr", "jellyfin", "library-updates", "deluge", "configarr", "seerr", "bazarr", "maintainerr", "prowlarr-sync",
    ]


def test_each_step_waits_for_the_apps_it_changes(wire_apps):
    waits = {name: apps for name, apps, _ in wire_apps.STEPS}
    assert waits["library-updates"] == ["sonarr", "radarr"]
    assert waits["prowlarr-sync"] == ["prowlarr", "sonarr", "radarr"]
    assert waits["configarr"] == []


def test_wiring_runs_every_step_in_order(wire_apps, http):
    http.on("GET", "/ping", None)
    ran = []
    assert wire_apps.run_steps([(name, ["prowlarr"], recording(ran, name)) for name in ("first", "second", "third")]) == 0
    assert ran == ["first", "second", "third"]


def test_a_failing_step_does_not_stop_the_others_but_fails_the_run_and_is_named(wire_apps, capsys):
    ran = []
    steps = [("first", [], recording(ran, "first")), ("second", [], recording(ran, "second", wire_apps.WiringError("no library Cartoons"))), ("third", [], recording(ran, "third"))]
    assert wire_apps.run_steps(steps) == 1
    assert ran == ["first", "second", "third"]
    errors = capsys.readouterr().err.splitlines()
    assert errors == ["second: no library Cartoons", "wiring failed: second"]


def test_a_step_that_crashes_does_not_stop_the_others_and_is_named(wire_apps, capsys):
    ran = []
    steps = [("first", [], recording(ran, "first", KeyError("definition"))), ("second", [], recording(ran, "second"))]
    assert wire_apps.run_steps(steps) == 1
    assert ran == ["first", "second"]
    assert "first: stopped by KeyError: 'definition'" in capsys.readouterr().err


def test_a_failed_configarr_run_is_named_and_the_rest_still_run(wire_apps, capsys):
    ran = []
    steps = [("configarr", [], recording(ran, "configarr", subprocess.CalledProcessError(1, "configarr.sh"))), ("seerr", [], recording(ran, "seerr"))]
    assert wire_apps.run_steps(steps) == 1
    assert ran == ["configarr", "seerr"]
    assert capsys.readouterr().err.splitlines() == ["wiring failed: configarr"]


def test_wiring_with_no_steps_succeeds(wire_apps):
    assert wire_apps.run_steps([]) == 0


def test_a_step_waits_until_its_app_answers(wire_apps, http):
    http.on("GET", "http://localhost:9696/ping", refused(), http_error(503, "starting"), None)
    ran = []
    assert wire_apps.run_steps([("prowlarr", ["prowlarr"], recording(ran, "prowlarr"))]) == 0
    assert len(http.requests) == 3
    assert ran == ["prowlarr"]


def test_an_app_that_never_answers_is_skipped_and_named_and_the_rest_still_run(wire_apps, http, monkeypatch, capsys):
    monkeypatch.setenv("WIRE_WAIT_SECONDS", "0")
    http.on("GET", "http://localhost:9696/ping", refused())
    http.on("GET", "http://localhost:8096/System/Info/Public", None)
    ran = []
    steps = [("prowlarr", ["prowlarr"], recording(ran, "prowlarr")), ("jellyfin", ["jellyfin"], recording(ran, "jellyfin"))]
    assert wire_apps.run_steps(steps) == 1
    assert ran == ["jellyfin"]
    assert "prowlarr: prowlarr not answering after 0s; skipped its wiring" in capsys.readouterr().err


def test_steps_without_an_app_of_their_own_do_not_wait(wire_apps, http):
    ran = []
    assert wire_apps.run_steps([("configarr", [], recording(ran, "configarr"))]) == 0
    assert http.requests == []


def test_a_step_that_changes_two_apps_waits_for_both(wire_apps, http):
    http.on("GET", "http://localhost:8989/ping", None)
    http.on("GET", "http://localhost:7878/ping", None)
    wire_apps.run_steps([("library-updates", ["sonarr", "radarr"], lambda: None)])
    assert {request.host for request in http.requests} == {"localhost:8989", "localhost:7878"}


def test_jellyfin_is_wired_once_its_api_answers_which_it_does_after_its_health_check(wire_apps, http):
    http.on("GET", "http://localhost:8096/System/Info/Public", http_error(503, "starting"), None)
    ran = []
    assert wire_apps.run_steps([("jellyfin", ["jellyfin"], recording(ran, "jellyfin"))]) == 0
    assert [request.path for request in http.requests] == ["/System/Info/Public", "/System/Info/Public"]


def test_an_apps_address_can_be_set_for_this_machine(wire_apps, http, monkeypatch):
    monkeypatch.setenv("SEERR_URL", "http://seerr.example:5055/")
    http.on("GET", "http://seerr.example:5055/api/v1/status", None)
    assert wire_apps.run_steps([("seerr", ["seerr"], lambda: None)]) == 0


def test_apps_get_five_minutes_to_answer_enough_for_a_database_migration_on_a_raspberry_pi(wire_apps, monkeypatch):
    monkeypatch.delenv("WIRE_WAIT_SECONDS", raising=False)
    assert wire_apps.wait_seconds() == 300


def test_an_engine_step_calls_its_modules_function(wire_apps, monkeypatch):
    called = []
    module = wire_apps.importlib.import_module("prowlarr")
    monkeypatch.setattr(module, "sync_again", lambda: called.append("sync_again"))
    dict((name, step) for name, _, step in wire_apps.STEPS)["prowlarr-sync"]()
    assert called == ["sync_again"]


def test_the_configarr_step_runs_its_script_and_fails_when_it_does(wire_apps, monkeypatch):
    commands = []

    def run(command, check):
        commands.append(command)
        raise subprocess.CalledProcessError(1, command)

    monkeypatch.setattr(subprocess, "run", run)
    with pytest.raises(subprocess.CalledProcessError):
        wire_apps.configarr()
    assert commands[0][0].endswith("scripts/wire/configarr.sh")


def test_an_app_that_answers_garbage_while_starting_is_waited_for_like_one_that_does_not_answer(wire_apps, http):
    http.on("GET", "http://localhost:9696/ping", lambda: wire_apps.http.client.BadStatusLine("garbage"), None)
    ran = []
    assert wire_apps.run_steps([("prowlarr", ["prowlarr"], recording(ran, "prowlarr"))]) == 0
    assert ran == ["prowlarr"]
