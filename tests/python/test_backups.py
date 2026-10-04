import fcntl
import os
import signal
from unittest import mock

import pytest

from conftest import done, fresh_engine, refused


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


PINGS = "https://hc-ping.com/pk/testinst-backup"


@pytest.fixture
def backup(installed, commands, http, monkeypatch):
    commands.on(["restic"])
    commands.on(["restic", "snapshots"], done(stdout="[]"))
    commands.on(["docker", "compose"])
    commands.on(["docker", "compose", "ps", "--status", "running", "--services"], done(stdout="jellyfin\nsonarr\n"))
    for suffix in ("/start", "/fail", ""):
        http.on("GET", f"{PINGS}{suffix}?create=1", {})
    module = fresh_engine("engine.backups")
    monkeypatch.setattr(module.installation, "short_hostname", lambda: "pi")
    monkeypatch.setattr(module.role, "machine_id", lambda: "this-machine")
    monkeypatch.setattr(module.role, "is_main", lambda: 0)
    return module


def pings(http):
    return [r.path.split("testinst-backup")[1] for r in http.requests]


def test_backup_stops_the_stack_snapshots_restarts_what_was_running_prunes_and_pings_success(backup, commands, http):
    assert backup.main([]) == 0
    assert commands.index("restic", "unlock") < commands.index("docker", "compose", "stop") < commands.index("restic", "backup")
    assert commands.index("restic", "backup") < commands.index("docker", "compose", "start", "jellyfin", "sonarr") < commands.index("restic", "forget")
    assert commands.did("restic", "forget", "--retry-lock", "2h", "--host", "testinst", "--prune", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6")
    assert pings(http) == ["/start?create=1", "?create=1"]


def test_backup_uses_the_excludes_file_and_tags_the_snapshot_with_this_machine(backup, commands, installed):
    backup.main([])
    snapshot = next(command for command in commands.ran if command.args[:2] == ["restic", "backup"])
    assert snapshot.args == [
        "restic", "backup", "--retry-lock", "2h", "--host", "testinst", "--tag", "machine:this-machine", "--tag", "machine-name:pi",
        "--tag", "nightly", "--exclude-file", backup.EXCLUDES, "volumes",
    ]
    assert backup.EXCLUDES.endswith("scripts/backup-excludes.txt")


def test_backup_snapshots_volumes_from_the_data_directory(backup, commands, installed):
    seen = {}
    commands.on(["restic", "backup"], done(then=lambda: seen.update(cwd=os.getcwd())))
    backup.main([])
    assert seen["cwd"] == os.path.realpath(installed.data)


def test_backup_restarts_services_when_restic_fails_and_pings_fail(backup, commands, http):
    commands.on(["restic", "backup"], done(returncode=1))
    assert backup.main([]) == 1
    assert commands.count("docker", "compose", "start", "jellyfin", "sonarr") == 1
    assert not commands.did("restic", "forget")
    assert pings(http) == ["/start?create=1", "/fail?create=1"]


def test_backup_starts_nothing_when_nothing_was_running(backup, commands):
    commands.on(["docker", "compose", "ps"], done(stdout=""))
    assert backup.main([]) == 0
    assert not commands.did("docker", "compose", "start")


def test_backup_pings_fail_when_restarting_the_stack_fails(backup, commands, http):
    commands.on(["docker", "compose", "start"], done(returncode=1))
    assert backup.main([]) == 1
    assert pings(http)[-1] == "/fail?create=1"


def test_backup_pings_fail_when_it_cannot_even_list_the_running_services(backup, commands, http):
    commands.on(["docker", "compose", "ps"], done(returncode=1))
    assert backup.main([]) == 1
    assert not commands.did("docker", "compose", "stop")
    assert pings(http)[-1] == "/fail?create=1"


def test_a_terminated_backup_starts_the_services_releases_the_lock_and_pings_fail(backup, commands, http, installed):
    previous = signal.signal(signal.SIGTERM, backup.program.stop_on_terminate)
    try:
        commands.on(["restic", "backup"], done(then=lambda: os.kill(os.getpid(), signal.SIGTERM)))
        with pytest.raises(SystemExit) as ended:
            backup.main([])
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert ended.value.code == 143
    assert commands.signals_to("restic", "backup") == [signal.SIGTERM]
    assert commands.did("docker", "compose", "start", "jellyfin", "sonarr")
    assert backup.running_backup_pid() == ""
    assert pings(http)[-1] == "/fail?create=1"


def test_an_interrupted_backup_starts_the_services_it_stopped(backup, commands):
    def interrupt():
        raise KeyboardInterrupt

    commands.on(["restic", "backup"], done(then=interrupt))
    assert backup.main([]) == 130
    assert commands.did("docker", "compose", "start", "jellyfin", "sonarr")


def test_a_secondary_never_stops_the_stack_or_uploads_and_reports_why(backup, commands, http, installed, monkeypatch, capsys):
    (installed.data / ".backup-main").touch()
    monkeypatch.setattr(backup.role, "is_main", lambda: backup.role.NOT_THE_MAIN)
    assert backup.main([]) == 1
    assert not commands.did("docker", "compose", "stop")
    assert not commands.did("restic", "backup")
    assert capsys.readouterr().err == "backup: another machine is testinst's main; this machine does not back up (make claim-backup-main makes it the main)\n"
    assert pings(http)[-1] == "/fail?create=1"
    assert not (installed.data / ".backup-main").exists()


def test_a_backup_repository_that_cannot_be_read_keeps_this_machine_the_main_and_says_so(backup, commands, installed, monkeypatch, capsys):
    (installed.data / ".backup-main").touch()
    monkeypatch.setattr(backup.role, "is_main", lambda: backup.role.UNREADABLE)
    assert backup.main([]) == 1
    assert capsys.readouterr().err == "backup: cannot read the backup repository to tell which machine is testinst's main; nothing was backed up\n"
    assert (installed.data / ".backup-main").exists()
    assert not commands.did("restic", "backup")


def test_a_claim_backs_up_even_where_another_machine_is_the_main(backup, commands, monkeypatch):
    monkeypatch.setattr(backup.role, "is_main", lambda: backup.role.NOT_THE_MAIN)
    backup.backup(claim=True)
    assert commands.did("restic", "backup")


def test_a_backup_refuses_to_start_while_another_one_is_running(backup, commands, http, installed, capsys):
    with open(installed.data / ".backup.lock", "a") as held:
        fcntl.flock(held, fcntl.LOCK_EX)
        held.write("4242")
        held.flush()
        assert backup.main([]) == 1
    assert capsys.readouterr().err == "backup: a backup is already running (process 4242)\n"
    assert not commands.did("restic", "backup")
    assert not commands.did("docker", "compose", "stop")
    assert http.requests == []


def test_a_lock_left_by_a_backup_that_no_longer_runs_is_taken_over(backup, commands, installed):
    (installed.data / ".backup.lock").write_text("999999\n")
    assert backup.main([]) == 0
    assert commands.did("restic", "backup")


def test_a_backup_holds_the_lock_while_restic_runs_and_releases_it_after(backup, commands):
    seen = {}
    commands.on(["restic", "backup"], done(then=lambda: seen.update(pid=backup.running_backup_pid())))
    backup.main([])
    assert seen["pid"] == str(os.getpid())
    assert backup.running_backup_pid() == ""


def test_the_lock_is_released_after_a_failed_backup(backup, commands):
    commands.on(["restic", "backup"], done(returncode=1))
    backup.main([])
    assert backup.running_backup_pid() == ""


def test_a_successful_backup_marks_this_machine_as_the_main_again(backup, installed):
    assert backup.main([]) == 0
    assert (installed.data / ".backup-main").exists()


def test_a_backup_that_gives_up_waiting_for_a_restic_lock_fails_with_restics_status(backup, commands, http, capsys):
    commands.on(["restic", "list", "locks"], done(stdout="5f3a9c\n"))
    commands.on(["restic", "cat", "lock"], done(stdout='{"time":"2026-09-30T04:37:23+01:00","exclusive":false,"hostname":"laptop","pid":6116}'))
    commands.on(["restic", "forget"], done(returncode=11))
    assert backup.main([]) == 11
    err = capsys.readouterr().err
    assert "laptop" in err and "6116" in err and "make unlock-backup" in err
    assert pings(http)[-1] == "/fail?create=1"


def test_a_ping_that_cannot_get_through_does_not_fail_the_backup(backup, commands, http, monkeypatch):
    monkeypatch.setattr(backup.healthchecks.time, "sleep", mock.Mock())
    for suffix in ("/start", ""):
        http.on("GET", f"{PINGS}{suffix}?create=1", refused())
    assert backup.main([]) == 0
    assert commands.did("restic", "forget")



def test_a_hung_up_backup_starts_the_services_releases_the_lock_and_pings_fail(backup, commands, http):
    previous = signal.signal(signal.SIGHUP, backup.program.stop_on_terminate)
    try:
        commands.on(["restic", "backup"], done(then=lambda: os.kill(os.getpid(), signal.SIGHUP)))
        with pytest.raises(SystemExit) as ended:
            backup.main([])
    finally:
        signal.signal(signal.SIGHUP, previous)
    assert ended.value.code == 129
    assert commands.did("docker", "compose", "start", "jellyfin", "sonarr")
    assert backup.running_backup_pid() == ""
    assert pings(http)[-1] == "/fail?create=1"


def test_a_second_signal_while_restarting_the_services_does_not_stop_the_restart(backup, commands, http):
    previous = signal.signal(signal.SIGTERM, backup.program.stop_on_terminate)
    restarted = []
    try:
        commands.on(["restic", "backup"], done(then=lambda: os.kill(os.getpid(), signal.SIGTERM)))
        commands.on(["docker", "compose", "start"], done(then=lambda: (os.kill(os.getpid(), signal.SIGTERM), restarted.append(True))))
        with pytest.raises(SystemExit):
            backup.main([])
        assert signal.getsignal(signal.SIGTERM) is backup.program.stop_on_terminate
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert restarted == [True]
    assert pings(http)[-1] == "/fail?create=1"



def test_restic_and_docker_hold_the_backup_lock_too_so_it_outlives_a_killed_backup(backup, commands):
    backup.main([])
    holding = {tuple(command.args[:3]): command.pass_fds for command in commands.ran}
    lock = holding[("restic", "backup", "--retry-lock")]
    assert len(lock) == 1
    assert holding[("docker", "compose", "--project-name")] == lock
    assert holding[("restic", "forget", "--retry-lock")] == lock


def test_a_successful_backup_refreshes_the_mains_marker_time(backup, installed):
    marker = installed.data / ".backup-main"
    marker.touch()
    os.utime(marker, (0, 0))
    assert backup.main([]) == 0
    assert marker.stat().st_mtime > 0
