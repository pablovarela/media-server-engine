import io
import os
import sys

import pytest

from conftest import done, fresh_engine


@pytest.fixture
def claim(installed, commands, monkeypatch):
    commands.on(["restic"])
    module = fresh_engine("engine.claim")
    claims = []

    def backed_up(claim=False):
        claims.append(claim)

    monkeypatch.setattr(module.backups, "backup", backed_up)
    monkeypatch.setattr(module.role, "is_main", lambda: 0)
    monkeypatch.setattr(module.role, "describe_main", lambda: "pi, last backup 2026-09-30 04:30")
    module.claims = claims
    return module


def another_main(claim, monkeypatch):
    monkeypatch.setattr(claim.role, "is_main", lambda: claim.role.NOT_THE_MAIN)


def typed(monkeypatch, text):
    monkeypatch.setattr(sys, "stdin", io.StringIO(text))


def test_claiming_runs_one_backup_that_bypasses_the_main_check(claim):
    assert claim.main([]) == 0
    assert claim.claims == [True]


def test_claiming_marks_this_machine_as_the_main(claim, installed, capsys):
    claim.main([])
    assert (installed.data / ".backup-main").exists()
    assert capsys.readouterr().out.endswith("This machine is now testinst's main; backups from any other machine are refused.\n")


def test_a_failed_claim_leaves_the_machine_unmarked(claim, installed, monkeypatch):
    failure = claim.commands.CommandFailed

    def failed(**options):
        raise failure(1)

    monkeypatch.setattr(claim.backups, "backup", failed)
    assert claim.main([]) == 1
    assert not (installed.data / ".backup-main").exists()


def test_claiming_creates_the_backup_repository_when_it_does_not_exist_yet(claim, commands, monkeypatch, capsys):
    commands.on(["restic", "cat", "config"], done(returncode=10))
    order = []
    monkeypatch.setattr(claim.backups, "backup", lambda claim=False: order.append("backup"))
    commands.on(["restic", "init"], done(then=lambda: order.append("init")))
    os.environ["RESTIC_REPOSITORY"] = "b2:bucket:testinst"
    assert claim.main([]) == 0
    assert order == ["init", "backup"]
    assert "Creating the backup repository b2:bucket:testinst\n" in capsys.readouterr().out


def test_claiming_leaves_an_existing_backup_repository_as_it_is(claim, commands):
    claim.main([])
    assert not commands.did("restic", "init")


def test_claiming_from_another_main_shows_it_and_asks_first_and_no_changes_nothing(claim, installed, monkeypatch, capsys):
    another_main(claim, monkeypatch)
    typed(monkeypatch, "n\n")
    assert claim.main([]) == 1
    err = capsys.readouterr().err
    assert "testinst's main is pi, last backup 2026-09-30 04:30. Taking over makes it refuse to back up.\n" in err
    assert "Make this machine the main instead? (y/n) [n]: " in err
    assert err.endswith("claim-backup-main: nothing was claimed\n")
    assert claim.claims == []
    assert not (installed.data / ".backup-main").exists()


def test_pressing_enter_keeps_the_other_main(claim, monkeypatch):
    another_main(claim, monkeypatch)
    typed(monkeypatch, "\n")
    assert claim.main([]) == 1
    assert claim.claims == []


def test_claiming_from_another_main_goes_ahead_when_the_answer_is_yes(claim, monkeypatch):
    another_main(claim, monkeypatch)
    typed(monkeypatch, "y\n")
    assert claim.main([]) == 0
    assert claim.claims == [True]


def test_a_claim_already_confirmed_does_not_ask_again(claim, monkeypatch):
    another_main(claim, monkeypatch)
    typed(monkeypatch, "")
    os.environ["CLAIM_CONFIRMED"] = "1"
    assert claim.main([]) == 0
    assert claim.claims == [True]


def test_a_repository_that_cannot_be_read_claims_nothing(claim, monkeypatch, capsys):
    monkeypatch.setattr(claim.role, "is_main", lambda: claim.role.UNREADABLE)
    assert claim.main([]) == 1
    assert capsys.readouterr().err == "claim-backup-main: cannot read the backup repository to tell which machine is testinst's main; nothing was claimed\n"
    assert claim.claims == []
