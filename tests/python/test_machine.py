import io
import os
import shlex
import shutil
import sys

import pytest

from conftest import done, fresh_engine


@pytest.fixture
def machine(installed, commands, tmp_path, monkeypatch):
    os.environ.update(SYSTEMD_RUNTIME_DIR=str(tmp_path), MEDIA_SERVER_HOST="homeserver.local")
    for name in ("RESTORE_FROM_BACKUP", "MACHINE_ROLE"):
        os.environ.pop(name, None)
    monkeypatch.setattr(shutil, "which", lambda name: f"/usr/bin/{name}")
    commands.on(["check-tools.sh"])
    commands.on(["sops", "exec-env"])
    commands.on(["sops", "exec-env", "backup.sops.env", "restic snapshots --no-lock --latest 1 --json"], done(stdout='[{"id": "s1"}]'))
    commands.on(["engine-run"])
    commands.on(["engine-run", "urls"], done(stdout="Jellyfin     http://homeserver.local:8096\n"))
    module = fresh_engine("engine.machine")
    return module


def answering(monkeypatch, text):
    monkeypatch.setattr(sys, "stdin", io.StringIO(text))


def inner(commands):
    return [shlex.split(command.args[3]) for command in commands.ran if command.args[:2] == ["sops", "exec-env"]]


def engine_runs(commands):
    return [command.args[1:] for command in commands.ran if command.args[0].endswith("/engine-run")]


def summary(capsys):
    out = capsys.readouterr().out
    return out[out.index("testinst is ready"):]


def test_a_new_installation_is_not_restored_even_if_the_repository_has_snapshots(machine, commands, monkeypatch):
    answering(monkeypatch, "y\n")
    assert machine.main([]) == 0
    assert not any(args[-1] == "restore" for args in inner(commands))


def test_a_joining_machine_restores_the_latest_backup_before_bringing_the_stack_up(machine, commands, monkeypatch):
    os.environ["RESTORE_FROM_BACKUP"] = "1"
    answering(monkeypatch, "n\n")
    machine.main([])
    restore = next(i for i, command in enumerate(commands.ran) if command.args[:2] == ["sops", "exec-env"] and command.args[3].endswith(" restore"))
    assert restore < commands.index("engine-run", "update")
    assert commands.did("sops", "exec-env", "backup.sops.env", "restic snapshots --no-lock --latest 1 --json")


def test_nothing_is_restored_when_the_installation_has_no_backups_yet(machine, commands, monkeypatch):
    os.environ["RESTORE_FROM_BACKUP"] = "1"
    commands.on(["sops", "exec-env", "backup.sops.env", "restic snapshots --no-lock --latest 1 --json"], done(stdout="[]"))
    answering(monkeypatch, "n\n")
    machine.main([])
    assert not any(args[-1] == "restore" for args in inner(commands))


def test_the_main_runs_its_first_update_as_the_main_installs_timers_and_claims_before_the_backup_timers(machine, commands, monkeypatch, installed):
    answering(monkeypatch, "y\n")
    assert machine.main([]) == 0
    update = next(command for command in commands.ran if command.args[1:] == ["update"])
    assert update.env["MACHINE_ROLE"] == "main"
    assert engine_runs(commands)[0:2] == [["update"], ["install-timers", "media-update", "media-download-cleanup"]]
    claim = next(command for command in commands.ran if command.args[:3] == ["sops", "exec-env", f"{installed.config}/secrets/healthchecks.sops.env"])
    assert claim.env["CLAIM_CONFIRMED"] == "1"
    assert shlex.split(claim.args[3]) == ["sops", "exec-env", f"{installed.config}/secrets/backup.sops.env", f"{installed.engine}/scripts/engine-run", "claim-backup-main"]
    assert inner(commands)[-1] == [f"{installed.engine}/scripts/engine-run", "install-timers", "media-backup", "media-verify"]


def test_a_machine_that_stays_secondary_runs_its_first_update_with_its_own_role(machine, commands, monkeypatch):
    answering(monkeypatch, "n\n")
    machine.main([])
    update = next(command for command in commands.ran if command.args[1:] == ["update"])
    assert (update.env or {}).get("MACHINE_ROLE") is None
    assert not any("claim-backup-main" in " ".join(args) for args in inner(commands))


def test_the_main_of_a_machine_without_systemd_still_claims_so_manual_backups_work(machine, commands, monkeypatch):
    monkeypatch.setattr(shutil, "which", lambda name: None)
    answering(monkeypatch, "y\n")
    assert machine.main([]) == 0
    assert any(args[-1] == "claim-backup-main" for args in inner(commands))
    assert not any(args[0] == "install-timers" for args in engine_runs(commands))


def test_the_main_question_names_the_current_main_and_defaults_to_no(machine, commands, monkeypatch, capsys):
    commands.on(["sops", "exec-env", "backup.sops.env", "engine-run backup-role is-main"], done(returncode=1))
    commands.on(["sops", "exec-env", "backup.sops.env", "engine-run backup-role describe-main"], done(stdout="pi, last backup 2026-09-30 04:30\n"))
    answering(monkeypatch, "\n")
    machine.main([])
    err = capsys.readouterr().err
    assert "another machine is testinst's main (pi, last backup 2026-09-30 04:30); say yes only to take over from it\n" in err
    assert "Make this machine testinst's main, the one that backs up? (y/n) [n]: " in err
    assert not any(args[-1] == "claim-backup-main" for args in inner(commands))


def test_setup_ends_with_a_summary_of_where_the_installation_lives_and_how_to_use_it(machine, monkeypatch, installed, capsys):
    answering(monkeypatch, "y\n")
    machine.main([])
    assert summary(capsys) == (
        "testinst is ready on homeserver.local.\n\n"
        f"It lives in {installed.engine.parent}; run make from there.\n\n"
        "Apps (make urls lists them again):\n"
        "  Jellyfin     http://homeserver.local:8096\n"
        "Their logins: make logins (shows the passwords on this terminal).\n\n"
        "Everyday commands:\n"
        "  make configure    change settings and secrets, then make update applies them\n"
        "  make update       apply config changes and update the apps\n"
        "  make media-stop   stop the apps; make media-start starts them again\n"
        "  make backup-now   back up now\n\n"
        "This machine updates itself daily at 05:00 and removes fake downloads every 15 minutes.\n"
        "It is the main: it backs up daily at 04:30 and checks the backups on Sundays at 05:30.\n"
        "================================================================\n\n"
        "Go to the installation, to run make from it:\n"
        f"  cd {installed.engine.parent}\n"
    )


def test_a_machine_that_is_not_the_main_is_told_the_main_backs_up(machine, monkeypatch, capsys):
    answering(monkeypatch, "n\n")
    machine.main([])
    text = summary(capsys)
    assert "Another machine backs up; this one never does.\n" in text
    assert "backup-now" not in text


def test_without_systemd_the_summary_says_nothing_runs_on_its_own(machine, monkeypatch, capsys):
    monkeypatch.setattr(shutil, "which", lambda name: None)
    answering(monkeypatch, "n\n")
    machine.main([])
    text = summary(capsys)
    assert "This machine has no systemd, so nothing runs on its own:\n  run make update after config changes.\nAnother machine backs up; this one never does.\n" in text
    assert "04:30" not in text


def test_without_systemd_the_main_is_told_to_back_up_by_hand(machine, monkeypatch, capsys):
    monkeypatch.setattr(shutil, "which", lambda name: None)
    answering(monkeypatch, "y\n")
    machine.main([])
    assert "  run make update after config changes, and make backup-now to back up.\n" in summary(capsys)


def test_the_tools_are_checked_first(machine, commands, monkeypatch):
    answering(monkeypatch, "n\n")
    machine.main([])
    assert commands.index("check-tools.sh") < commands.index("engine-run", "update")


def test_a_repository_that_cannot_be_read_counts_as_having_no_backups_to_restore(machine, commands, monkeypatch):
    os.environ["RESTORE_FROM_BACKUP"] = "1"
    commands.on(["sops", "exec-env", "backup.sops.env", "restic snapshots --no-lock --latest 1 --json"], done(stderr="Fatal: wrong password\n", returncode=1))
    answering(monkeypatch, "n\n")
    assert machine.main([]) == 0
    assert not any(args[-1] == "restore" for args in inner(commands))



def test_looking_for_backups_keeps_restics_complaints_out_of_the_setup(machine, commands, monkeypatch, capsys):
    os.environ["RESTORE_FROM_BACKUP"] = "1"
    commands.on(["sops", "exec-env", "backup.sops.env", "restic snapshots --no-lock --latest 1 --json"], done(stderr="Fatal: unable to open config file\n", returncode=1))
    answering(monkeypatch, "n\n")
    machine.main([])
    assert "Fatal" not in capsys.readouterr().err



def test_the_summary_starts_even_when_the_app_list_cannot_be_shown(machine, commands, monkeypatch, capsys):
    commands.on(["engine-run", "urls"], done(returncode=1))
    answering(monkeypatch, "n\n")
    assert machine.main([]) == 1
    assert "testinst is ready on homeserver.local.\n" in capsys.readouterr().out
