import json

import pytest

from conftest import done, fresh_engine

LOCK = {"time": "2026-09-30T04:37:23+01:00", "exclusive": False, "hostname": "laptop", "pid": 6116}


@pytest.fixture
def restic(installed):
    return fresh_engine("engine.restic")


def locks_held(commands):
    commands.on(["restic", "list", "locks", "--no-lock"], done(stdout="5f3a9c\n"))
    commands.on(["restic", "cat", "lock", "5f3a9c", "--no-lock"], done(stdout=json.dumps(LOCK)))


def test_locks_are_described_by_kind_host_process_and_time(restic, commands):
    locks_held(commands)
    assert restic.describe_locks() == ["  shared lock from laptop (process 6116) since 2026-09-30T04:37:23+01:00"]


def test_a_lock_that_cannot_be_read_is_left_out(restic, commands):
    commands.on(["restic", "list", "locks", "--no-lock"], done(stdout="5f3a9c\n"))
    commands.on(["restic", "cat", "lock"], done(stderr="Fatal: no such lock\n", returncode=1))
    assert restic.describe_locks() == []


def test_a_restic_that_gave_up_on_a_lock_says_who_holds_it_and_how_to_clear_it(restic, commands, capsys):
    locks_held(commands)
    commands.on(["restic", "forget"], done(returncode=11))
    with pytest.raises(restic.commands.CommandFailed) as failed:
        restic.run_explaining_locks("forget", "--prune")
    assert failed.value.returncode == 11
    assert capsys.readouterr().err == (
        "restic gave up waiting for a lock on the backup repository. Locks held:\n"
        "  shared lock from laptop (process 6116) since 2026-09-30T04:37:23+01:00\n"
        "If none of those machines is running restic now, remove every lock with: make unlock-backup ALL=1\n"
    )


def test_other_restic_failures_are_not_explained_as_locks(restic, commands, capsys):
    commands.on(["restic", "check"], done(returncode=1))
    with pytest.raises(restic.commands.CommandFailed):
        restic.run_explaining_locks("check")
    assert "lock" not in capsys.readouterr().err


def test_the_installations_own_snapshots_are_used_when_it_has_any(restic, commands):
    commands.on(["restic", "snapshots", "--no-lock", "--host", "testinst", "--json"], done(stdout='[{"id": "x"}]'))
    assert restic.snapshot_filter() == ["--host", "testinst"]


@pytest.mark.parametrize("answer", [done(stdout="[]"), done(stdout="null"), done(returncode=1)])
def test_without_snapshots_of_its_own_any_host_will_do(restic, commands, answer):
    commands.on(["restic", "snapshots"], answer)
    assert restic.snapshot_filter() == []
