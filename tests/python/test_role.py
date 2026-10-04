import json
import os

import pytest

from conftest import done, fresh_engine


@pytest.fixture
def role(installed, tmp_path):
    machine_id = tmp_path / "machine-id"
    machine_id.write_text("this-machine\n")
    os.environ["MACHINE_ID_FILE"] = str(machine_id)
    return fresh_engine("engine.role")


def snapshots(commands, *snapshots):
    commands.on(["restic", "snapshots", "--no-lock", "--host", "testinst", "--latest", "1", "--json"], done(stdout=json.dumps(list(snapshots))))


def tagged(machine, time="2026-09-30T04:30:14Z", *extra):
    return {"time": time, "hostname": "testinst", "tags": ["nightly", f"machine:{machine}", *extra]}


def test_the_first_machine_of_a_new_installation_is_the_main(role, commands):
    snapshots(commands)
    assert role.main(["is-main"]) == 0


def test_the_machine_that_made_the_latest_snapshot_is_the_main(role, commands):
    snapshots(commands, tagged("other-machine", "2026-09-29T04:30:00Z"), tagged("this-machine"))
    assert role.main(["is-main"]) == 0


def test_a_machine_is_not_the_main_when_another_made_the_latest_snapshot(role, commands):
    snapshots(commands, tagged("other-machine"))
    assert role.main(["is-main"]) == 1


def test_snapshots_are_looked_up_for_the_installation_without_locking_the_repository(role, commands):
    snapshots(commands)
    role.main(["is-main"])
    assert commands.ran[-1].args == ["restic", "snapshots", "--no-lock", "--host", "testinst", "--latest", "1", "--json"]


def test_main_machine_prints_the_latest_snapshots_machine(role, commands, capsys):
    snapshots(commands, tagged("other-machine"))
    assert role.main(["main-machine"]) == 0
    assert capsys.readouterr().out == "other-machine\n"


def test_machine_id_reads_the_system_machine_id(role, capsys):
    assert role.main(["machine-id"]) == 0
    assert capsys.readouterr().out == "this-machine\n"


def test_machine_id_generates_and_keeps_an_id_when_the_system_has_none(role, installed, capsys):
    os.environ["MACHINE_ID_FILE"] = str(installed.data / "missing")
    first = role.machine_id()
    assert len(first) == 32
    assert role.machine_id() == first
    assert (installed.data / ".machine-id").read_text().strip() == first


def test_a_repository_that_does_not_exist_yet_has_no_main_and_is_not_reported_as_an_error(role, commands, capsys):
    commands.on(["restic", "snapshots"], done(stderr='{"message_type":"exit_error","code":10}\n', returncode=10))
    assert role.main(["is-main"]) == 0
    assert capsys.readouterr() == ("", "")


def test_a_repository_that_cannot_be_read_is_not_taken_to_mean_this_machine_is_the_main(role, commands):
    commands.on(["restic", "snapshots"], done(returncode=1))
    assert role.main(["is-main"]) == 2


def test_other_restic_errors_are_still_shown(role, commands, capsys):
    commands.on(["restic", "snapshots"], done(stderr="Fatal: wrong password\n", returncode=12))
    assert role.main(["is-main"]) == 2
    assert "wrong password" in capsys.readouterr().err


def test_an_unreadable_answer_from_restic_is_a_repository_that_cannot_be_read(role, commands):
    commands.on(["restic", "snapshots"], done(stdout="not json"))
    assert role.main(["is-main"]) == 2


def test_the_main_is_described_by_its_machine_name_and_the_time_of_its_last_backup(role, commands, capsys):
    snapshots(commands, tagged("other", "2026-09-30T04:30:14.66+01:00", "machine-name:pi"))
    assert role.main(["describe-main"]) == 0
    assert capsys.readouterr().out == "pi, last backup 2026-09-30 04:30\n"


def test_a_main_whose_snapshots_carry_no_machine_name_is_described_by_its_id(role, commands, capsys):
    snapshots(commands, tagged("other-machine"))
    role.main(["describe-main"])
    assert capsys.readouterr().out == "machine other-machine, last backup 2026-09-30 04:30\n"


def test_main_machine_fails_with_restics_status_when_the_repository_cannot_be_read(role, commands):
    commands.on(["restic", "snapshots"], done(returncode=12))
    assert role.main(["main-machine"]) == 12


def test_an_unknown_command_shows_the_usage(role, capsys):
    assert role.main(["whoami"]) == 1
    assert capsys.readouterr().err == "backup-role: usage: backup-role is-main|main-machine|describe-main|machine-id\n"


def test_each_status_is_explained(role):
    assert role.explain(role.NOT_THE_MAIN) == "another machine is testinst's main"
    assert role.explain(role.UNREADABLE) == "cannot read the backup repository to tell which machine is testinst's main"


@pytest.mark.parametrize("command", ["describe-main", "main-machine"])
def test_without_snapshots_there_is_no_main_to_print(role, commands, capsys, command):
    snapshots(commands)
    assert role.main([command]) == 0
    assert capsys.readouterr().out == ""
