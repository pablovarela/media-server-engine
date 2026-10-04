import os
import signal

import pytest

from conftest import fresh_engine


@pytest.fixture
def program(dirs, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    return fresh_engine("engine.program")


def test_a_program_that_ends_well_exits_0_or_with_the_status_it_returns(program):
    assert program.run("backup-role", lambda argv: None, []) == 0
    assert program.run("backup-role", lambda argv: 2, []) == 2


def test_a_stop_is_reported_under_the_programs_name(program, capsys):
    def stop(argv):
        raise program.commands.Stop("a backup is already running (process 4242)")

    assert program.run("backup", stop, []) == 1
    assert capsys.readouterr().err == "backup: a backup is already running (process 4242)\n"


def test_a_failed_command_ends_the_program_with_its_status(program):
    def fail(argv):
        raise program.commands.CommandFailed(11)

    assert program.run("backup", fail, []) == 11


def test_a_file_error_is_one_line_under_the_programs_name(program, capsys):
    def missing(argv):
        open("/nonexistent/engine.env")

    assert program.run("restore", missing, []) == 1
    assert capsys.readouterr().err == "restore: [Errno 2] No such file or directory: '/nonexistent/engine.env'\n"


def test_ctrl_c_ends_the_program_as_the_shell_does(program):
    def interrupted(argv):
        raise KeyboardInterrupt

    assert program.run("backup", interrupted, []) == 130


def test_the_arguments_reach_the_program(program):
    seen = []
    program.run("unlock-backup", seen.append, ["--remove-all"])
    assert seen == [["--remove-all"]]


def test_starting_keeps_the_api_keys_from_the_commands_it_runs(program):
    os.environ.update(HEALTHCHECKS_PING_KEY="pk", HEALTHCHECKS_API_KEY="read", HEALTHCHECKS_MANAGE_KEY="write")
    previous = signal.getsignal(signal.SIGTERM)
    try:
        program.start()
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert os.environ["HEALTHCHECKS_PING_KEY"] == "pk"
    assert "HEALTHCHECKS_API_KEY" not in os.environ
    assert "HEALTHCHECKS_MANAGE_KEY" not in os.environ


def test_starting_exports_the_directories(program, monkeypatch):
    monkeypatch.setattr(os, "environ", {"ENGINE_DIR": "/home/me/home/engine"})
    previous = signal.getsignal(signal.SIGTERM)
    try:
        program.start()
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert os.environ["DATA_DIR"] == "/home/me/home/data"


def test_a_terminated_program_ends_through_its_finally_blocks_with_the_shells_status(program):
    with pytest.raises(SystemExit) as ended:
        program.stop_on_terminate(signal.SIGTERM, None)
    assert ended.value.code == 143
