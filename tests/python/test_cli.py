import pytest

from conftest import fresh_engine


@pytest.fixture
def cli(dirs):
    return fresh_engine("engine.cli")


def test_every_command_runs_a_programs_main(cli):
    assert sorted(cli.COMMANDS) == ["backup", "backup-role", "claim-backup-main", "prune-stack-images", "restore", "unlock-backup", "update", "verify-backup"]
    assert all(callable(main) for main in cli.COMMANDS.values())


def test_a_command_gets_the_arguments_after_its_name(cli, monkeypatch):
    seen = []
    monkeypatch.setitem(cli.COMMANDS, "unlock-backup", lambda argv: seen.append(argv) or 4)
    assert cli.main(["unlock-backup", "--remove-all"]) == 4
    assert seen == [["--remove-all"]]


@pytest.mark.parametrize("argv", [[], ["backup-now"]])
def test_a_missing_or_unknown_command_lists_the_commands(cli, capsys, argv):
    assert cli.main(argv) == 1
    err = capsys.readouterr().err
    assert err.startswith("usage: engine-run <command> [arguments]\ncommands: backup, backup-role, claim-backup-main, ")
