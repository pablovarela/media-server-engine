import os
import signal
import time
from unittest import mock

import pytest

from conftest import done, fresh_engine

PINGED = "https://hc-ping.com/pk"


@pytest.fixture
def scheduled(installed, commands, http, monkeypatch):
    os.environ.pop("MACHINE_ROLE", None)
    for words in (["bootstrap.sh", "--pinned-tools"], ["check-tools.sh"], ["engine-run", "update"]):
        commands.on(words)
    for check in ("testinst-update", "testinst-update-laptop"):
        for suffix in ("/start", "", "/fail"):
            http.on("GET", f"{PINGED}/{check}{suffix}?create=1", {})
    monkeypatch.setattr(time, "sleep", mock.Mock())
    module = fresh_engine("engine.scheduled")
    monkeypatch.setattr(module.installation, "short_hostname", lambda: "laptop")
    return module


def pings(http):
    return [request.path for request in http.requests]


def test_the_main_installs_the_pinned_tools_checks_them_updates_and_pings_its_update_check(scheduled, installed, commands, http):
    (installed.data / ".backup-main").touch()
    assert scheduled.main([]) == 0
    assert pings(http) == ["/pk/testinst-update/start?create=1", "/pk/testinst-update?create=1"]
    assert commands.index("bootstrap.sh", "--pinned-tools") < commands.index("check-tools.sh") < commands.index("engine-run", "update")


def test_a_secondary_pings_its_own_update_check_and_its_update_knows_the_role(scheduled, http):
    assert scheduled.main([]) == 0
    assert pings(http) == ["/pk/testinst-update-laptop/start?create=1", "/pk/testinst-update-laptop?create=1"]
    assert os.environ["MACHINE_ROLE"] == "secondary"


def test_a_failed_update_pings_fail_and_ends_with_its_status(scheduled, commands, http):
    commands.on(["engine-run", "update"], done(returncode=3))
    assert scheduled.main([]) == 3
    assert pings(http)[-1] == "/pk/testinst-update-laptop/fail?create=1"


def test_broken_tools_stop_before_the_update_and_ping_fail(scheduled, commands, http):
    commands.on(["check-tools.sh"], done(returncode=1))
    assert scheduled.main([]) == 1
    assert not commands.did("engine-run", "update")
    assert pings(http) == ["/pk/testinst-update-laptop/start?create=1", "/pk/testinst-update-laptop/fail?create=1"]


def test_a_terminated_update_is_passed_the_signal_and_pings_fail(scheduled, commands, http):
    previous = signal.signal(signal.SIGTERM, scheduled.program.stop_on_terminate)
    commands.on(["engine-run", "update"], done(then=lambda: os.kill(os.getpid(), signal.SIGTERM)))
    try:
        with pytest.raises(SystemExit) as ended:
            scheduled.main([])
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert ended.value.code == 143
    assert commands.signals_to("engine-run", "update") == [signal.SIGTERM]
    assert pings(http)[-1] == "/pk/testinst-update-laptop/fail?create=1"


def test_outside_an_installation_nothing_runs_and_nothing_is_pinged(scheduled, installed, commands, http, tmp_path, capsys):
    os.environ["HOME"] = str(tmp_path / "home")
    (installed.config / "installation.env").unlink()
    assert scheduled.main([]) == 1
    assert "is not an installation" in capsys.readouterr().err
    assert pings(http) == []
    assert not commands.did("bootstrap.sh")


def test_a_stop_during_the_start_ping_still_pings_fail(scheduled, http):
    previous = signal.signal(signal.SIGTERM, scheduled.program.stop_on_terminate)

    def terminated_while_pinging():
        os.kill(os.getpid(), signal.SIGTERM)
        return ConnectionResetError("connection reset by peer")

    http.on("GET", f"{PINGED}/testinst-update-laptop/start?create=1", terminated_while_pinging)
    try:
        with pytest.raises(SystemExit) as ended:
            scheduled.main([])
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert ended.value.code == 143
    assert pings(http)[-1] == "/pk/testinst-update-laptop/fail?create=1"


def test_the_update_is_started_from_the_same_engine_as_the_tools(scheduled, commands):
    assert scheduled.main([]) == 0
    update = commands.ran[commands.index("engine-run", "update")]
    assert update.args[0] == os.path.join(scheduled.SCRIPTS_DIR, "engine-run")
