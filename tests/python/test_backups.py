import fcntl

import pytest

from conftest import fresh_engine


@pytest.fixture
def backups(dirs):
    return fresh_engine("engine.backups")


def test_no_lock_file_means_no_backup_is_running(backups):
    assert backups.running_backup_pid() == ""


def test_a_held_lock_names_the_running_backup(backups, dirs):
    with open(dirs.data / ".backup.lock", "a") as held:
        fcntl.flock(held, fcntl.LOCK_EX)
        held.write("4242")
        held.flush()
        assert backups.running_backup_pid() == "4242"


def test_a_lock_file_nobody_holds_is_left_by_a_backup_that_ended(backups, dirs):
    (dirs.data / ".backup.lock").write_text("999999")
    assert backups.running_backup_pid() == ""
