import io
import os
import signal
import subprocess
import sys

import pytest

from conftest import done, fresh_engine, timed_out


class Finished:
    def __init__(self, returncode):
        self.returncode = returncode

    def communicate(self, input=None, timeout=None):
        return None, None


@pytest.fixture
def commands_module():
    return fresh_engine("engine.commands")


def test_a_command_runs_with_its_output_shown_and_its_status_returned(commands_module, commands, capsys):
    commands.on(["docker", "compose", "up"], done(stdout="started\n", returncode=0))
    assert commands_module.run(["docker", "compose", "up"]) == 0
    assert capsys.readouterr().out == "started\n"


def test_a_failing_command_stops_the_program_with_its_status(commands_module, commands):
    commands.on(["bootstrap.sh"], done(returncode=3))
    with pytest.raises(commands_module.CommandFailed) as failed:
        commands_module.run(["/engine/scripts/bootstrap.sh"])
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

    monkeypatch.setattr(subprocess, "Popen", missing)
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

    def start(args, **options):
        seen["before"] = written.getvalue()
        return Finished(0)

    monkeypatch.setattr(subprocess, "Popen", start)
    print("healthchecks: set up home-update")
    commands_module.run(["bootstrap.sh"])
    assert seen["before"] == b"healthchecks: set up home-update\n"


def test_a_command_killed_by_a_signal_fails_with_the_status_the_shell_gives(commands_module, monkeypatch):
    monkeypatch.setattr(subprocess, "Popen", lambda args, **options: Finished(-9))
    assert commands_module.run(["docker", "compose", "up"], check=False) == 137


def test_captured_keeps_output_and_errors_apart(commands_module, commands):
    commands.on(["restic", "snapshots"], done(stdout="[]\n", stderr="Fatal: wrong password\n", returncode=12))
    assert commands_module.captured(["restic", "snapshots"]) == (12, "[]\n", "Fatal: wrong password\n")


def test_quiet_returns_the_status_and_shows_nothing(commands_module, commands, capsys):
    commands.on(["restic", "cat", "config"], done(stdout="{}\n", stderr="Fatal: repository does not exist\n", returncode=10))
    assert commands_module.quiet(["restic", "cat", "config"]) == 10
    assert capsys.readouterr() == ("", "")


def test_a_signal_while_a_command_runs_reaches_the_command_and_the_program_stops_once_it_has_exited(commands_module, commands):
    program = fresh_engine("engine.program")
    commands_module = program.commands
    previous = signal.signal(signal.SIGTERM, program.stop_on_terminate)
    try:
        commands.on(["restic", "backup"], done(then=lambda: os.kill(os.getpid(), signal.SIGTERM)))
        with pytest.raises(SystemExit) as ended:
            commands_module.run(["restic", "backup"])
        assert ended.value.code == 143
        assert commands.signals_to("restic", "backup") == [signal.SIGTERM]
        commands.on(["restic", "forget"])
        assert commands_module.run(["restic", "forget"]) == 0
    finally:
        signal.signal(signal.SIGTERM, previous)


def test_ctrl_c_waits_for_the_command_it_interrupted(commands_module, monkeypatch):
    waited = []

    class Interrupted:
        returncode = None

        def communicate(self, input=None, timeout=None):
            raise KeyboardInterrupt

        def wait(self):
            waited.append(True)
            self.returncode = -2
            return self.returncode

    monkeypatch.setattr(subprocess, "Popen", lambda args, **options: Interrupted())
    with pytest.raises(KeyboardInterrupt):
        commands_module.run(["restic", "backup"])
    assert waited == [True]


def test_text_can_be_given_to_a_command(commands_module, commands):
    commands.on(["sudo", "tee"])
    commands.on(["age-keygen", "-y"], done(stdout="age1public\n"))
    commands_module.run(["sudo", "tee", "/etc/systemd/system/x.timer"], input="[Timer]\n", discard_output=True)
    assert commands_module.output(["age-keygen", "-y"], input="AGE-SECRET-KEY-1\n") == "age1public\n"
    assert commands_module.captured(["age-keygen", "-y"], input="AGE-SECRET-KEY-2\n")[0] == 0
    assert commands.input_to("sudo", "tee") == ["[Timer]\n"]
    assert commands.input_to("age-keygen") == ["AGE-SECRET-KEY-1\n", "AGE-SECRET-KEY-2\n"]


def test_a_dialog_draws_on_the_terminal_and_returns_the_answer_it_writes_to_stderr(commands_module, commands, capsys):
    commands.on(["whiptail", "--menu"], done(stdout="drawn\n", stderr="Europe", returncode=0))
    assert commands_module.dialog(["whiptail", "--menu", "Choose a region."]) == (0, "Europe")
    assert capsys.readouterr().out == "drawn\n"


def test_a_dialog_that_is_escaped_returns_its_code_and_no_answer(commands_module, commands):
    commands.on(["whiptail"], done(returncode=255))
    assert commands_module.dialog(["whiptail", "--inputbox", "Jellyfin admin user"]) == (255, "")


def test_a_command_that_runs_past_its_timeout_is_killed_and_reported_as_timed_out(commands_module, commands):
    commands.on(["restic", "cat", "config"], timed_out(stderr="Load(<config/0000000000>) returned error, retrying\n"))
    code, _, errors = commands_module.captured(["restic", "-r", "b2:bucket", "cat", "config"], timeout=45)
    assert (code, errors) == (commands_module.TIMED_OUT, "Load(<config/0000000000>) returned error, retrying\n")
    assert commands.ran[-1].process.killed


def test_a_command_within_its_timeout_returns_as_usual(commands_module, commands):
    commands.on(["restic", "cat", "config"], done(stderr="Fatal: wrong password\n", returncode=12))
    assert commands_module.captured(["restic", "cat", "config"], timeout=45) == (12, "", "Fatal: wrong password\n")
