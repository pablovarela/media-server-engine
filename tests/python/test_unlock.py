import pytest

from conftest import done, fresh_engine


@pytest.fixture
def unlock(installed, commands):
    commands.on(["restic"])
    return fresh_engine("engine.unlock")


def test_unlock_removes_only_stale_locks_and_shows_the_ones_left(unlock, commands, capsys):
    commands.on(["restic", "list", "locks"], done(stdout="5f3a9c\n"))
    commands.on(["restic", "cat", "lock"], done(stdout='{"time":"2026-09-30T04:37:23+01:00","exclusive":false,"hostname":"laptop","pid":6116}'))
    assert unlock.main([]) == 0
    assert commands.ran[1].args == ["restic", "unlock"]
    assert capsys.readouterr().out == (
        "Locks left, held by restic processes that may still be running:\n"
        "  shared lock from laptop (process 6116) since 2026-09-30T04:37:23+01:00\n"
        "If none of those machines is running restic now, remove them with: make unlock-backup ALL=1\n"
    )


def test_unlock_says_so_when_no_lock_is_left(unlock, capsys):
    assert unlock.main([]) == 0
    assert capsys.readouterr().out == "no locks left on the backup repository\n"


def test_unlock_remove_all_removes_every_lock(unlock, commands):
    assert unlock.main(["--remove-all"]) == 0
    assert commands.did("restic", "unlock", "--remove-all")


def test_unlock_refuses_an_option_it_does_not_know(unlock, commands, capsys):
    assert unlock.main(["--everything"]) == 1
    assert not commands.did("restic", "unlock")
    assert capsys.readouterr().err == "unlock-backup: unknown option --everything; --remove-all removes every lock\n"
