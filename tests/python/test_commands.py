import io
import subprocess
import sys

import pytest

from conftest import done, fresh_engine


@pytest.fixture
def commands_module():
    return fresh_engine("engine.commands")


def test_a_command_runs_with_its_output_shown_and_its_status_returned(commands_module, commands, capsys):
    commands.on(["docker", "compose", "up"], done(stdout="started\n", returncode=0))
    assert commands_module.run(["docker", "compose", "up"]) == 0
    assert capsys.readouterr().out == "started\n"


def test_a_failing_command_stops_the_program_with_its_status(commands_module, commands):
    commands.on(["check-stack.sh"], done(returncode=3))
    with pytest.raises(commands_module.CommandFailed) as failed:
        commands_module.run(["/engine/scripts/check-stack.sh"])
    assert failed.value.returncode == 3


def test_a_failure_can_be_returned_instead(commands_module, commands):
    commands.on(["wire_apps.py"], done(returncode=2))
    assert commands_module.run(["wire_apps.py"], check=False) == 2


def test_output_is_captured_and_errors_can_be_discarded(commands_module, commands, capsys):
    commands.on(["git", "describe"], done(stdout="v1.0.0\n", stderr="noise\n"))
    assert commands_module.output(["git", "describe"], discard_errors=True) == "v1.0.0\n"
    assert capsys.readouterr() == ("", "")


def test_combined_output_holds_errors_too(commands_module, commands):
    commands.on(["docker", "compose", "pull"], done(stdout="a\n", stderr="toomanyrequests\n", returncode=1))
    assert commands_module.combined(["docker", "compose", "pull"]) == (1, "a\ntoomanyrequests\n")


def test_succeeds_says_whether_a_command_worked_and_shows_nothing(commands_module, commands, capsys):
    commands.on(["git", "remote", "get-url"], done(stderr="error: No such remote\n", returncode=2))
    assert commands_module.succeeds(["git", "remote", "get-url", "origin"]) is False
    assert capsys.readouterr() == ("", "")


def test_a_missing_program_fails_like_the_shell_does(commands_module, monkeypatch, capsys):
    def missing(args, **options):
        raise FileNotFoundError(2, "No such file or directory", args[0])

    monkeypatch.setattr(subprocess, "run", missing)
    with pytest.raises(commands_module.CommandFailed) as failed:
        commands_module.run(["docker", "compose", "up"])
    assert failed.value.returncode == 127
    assert capsys.readouterr().err == "docker: command not found\n"


def test_a_stop_names_the_program_unless_it_speaks_for_itself(commands_module):
    assert commands_module.Stop("a backup is still running").text("update") == "update: a backup is still running"
    assert commands_module.Stop("The config has changes", prefixed=False).text("update") == "The config has changes"


def test_what_the_program_printed_is_written_out_before_a_command_runs(commands_module, monkeypatch):
    written = io.BytesIO()
    monkeypatch.setattr(sys, "stdout", io.TextIOWrapper(written, write_through=False))
    seen = {}

    def run(args, **options):
        seen["before"] = written.getvalue()
        return subprocess.CompletedProcess(args, 0)

    monkeypatch.setattr(subprocess, "run", run)
    print("healthchecks: set up home-update")
    commands_module.run(["check-stack.sh"])
    assert seen["before"] == b"healthchecks: set up home-update\n"
