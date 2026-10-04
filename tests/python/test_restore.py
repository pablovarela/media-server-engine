import os

import pytest

from conftest import done, fresh_engine

RUNNING = ["docker", "ps", "-q", "--filter", "label=com.docker.compose.project=media-server"]


@pytest.fixture
def restore(installed, commands):
    commands.on(RUNNING, done(stdout=""))
    commands.on(["restic"])
    commands.on(["restic", "snapshots"], done(stdout="[]"))
    return fresh_engine("engine.restore")


def restored_into(commands):
    return next(command.args for command in commands.ran if command.args[:2] == ["restic", "restore"])


def test_restore_refuses_while_the_stack_is_running(restore, commands, capsys):
    commands.on(RUNNING, done(stdout="abc123\n"))
    assert restore.main([]) == 1
    assert capsys.readouterr().err == "restore: the stack is running; stop it with make media-stop first\n"
    assert not commands.did("restic", "restore")


def test_restore_refuses_when_it_cannot_tell_whether_the_stack_is_running(restore, commands, capsys):
    commands.on(RUNNING, done(returncode=1))
    assert restore.main([]) == 1
    assert capsys.readouterr().err.endswith("restore: cannot tell whether the stack is running (docker ps failed)\n")
    assert not commands.did("restic", "restore")


def test_restore_refuses_to_overwrite_existing_app_data(restore, commands, installed, capsys):
    (installed.data / "volumes" / "sonarr").mkdir(parents=True)
    assert restore.main([]) == 1
    assert capsys.readouterr().err == "restore: volumes/ already holds app data; run with --overwrite to replace it with the latest backup\n"
    assert not commands.did("restic", "restore")


def test_restore_into_an_empty_data_directory_needs_no_flag(restore, commands, installed):
    assert restore.main([]) == 0
    assert restored_into(commands) == ["restic", "restore", "latest:/volumes", "--target", f"{installed.data}/volumes", "--exclude", "configarr"]


def test_restore_treats_configarrs_cache_as_no_app_data(restore, installed):
    (installed.data / "volumes" / "configarr" / "repos").mkdir(parents=True)
    assert restore.main([]) == 0


def test_restore_overwrite_moves_existing_app_data_aside_instead_of_merging_into_it(restore, commands, installed, capsys):
    (installed.data / "volumes" / "sonarr" / "data").mkdir(parents=True)
    (installed.data / "volumes" / "sonarr" / "data" / "sonarr.db-wal").touch()
    assert restore.main(["--overwrite"]) == 0
    assert not (installed.data / "volumes" / "sonarr").exists()
    aside = [path for path in os.listdir(installed.data) if path.startswith("volumes.before-restore-")]
    assert len(aside) == 1
    assert (installed.data / aside[0] / "sonarr" / "data" / "sonarr.db-wal").exists()
    assert f"previous volumes/ kept in {installed.data}/{aside[0]}; delete it once the restore looks right\n" in capsys.readouterr().out
    assert restored_into(commands)[2] == "latest:/volumes"


def test_restore_without_overwrite_keeps_volumes_in_place(restore, installed):
    assert restore.main([]) == 0
    assert not [path for path in os.listdir(installed.data) if path.startswith("volumes.before-restore-")]


def test_restore_uses_the_installations_own_snapshots_when_it_has_any(restore, commands):
    commands.on(["restic", "snapshots"], done(stdout='[{"id": "x"}]'))
    assert restore.main([]) == 0
    assert restored_into(commands)[2:5] == ["latest:/volumes", "--host", "testinst"]


def test_restore_falls_back_to_the_latest_snapshot_of_any_host_when_the_installation_has_none_yet(restore, commands):
    assert restore.main([]) == 0
    assert "--host" not in restored_into(commands)


def test_restore_clears_stale_restic_locks_first(restore, commands):
    restore.main([])
    assert commands.index("restic", "unlock") < commands.index("restic", "restore")


def test_the_snapshot_count_for_the_host_filter_does_not_lock_the_repository(restore, commands):
    restore.main([])
    assert commands.did("restic", "snapshots", "--no-lock")
