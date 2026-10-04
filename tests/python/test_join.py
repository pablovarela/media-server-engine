import io
import os
import sys

import pytest

from conftest import done, fresh_engine, real

GOOD = "AGE-SECRET-KEY-GOOD"


@pytest.fixture
def join(dirs, commands, tmp_path, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ.update(HOME=str(tmp_path / "home"), INSTALL_DIR=str(dirs.engine.parent))
    for name in ("SOPS_AGE_KEY_FILE", "GITHUB_OWNER", "SKIP_RESTORE", "RESTORE_FROM_BACKUP"):
        os.environ.pop(name, None)
    os.rmdir(dirs.config)

    def cloned():
        (dirs.config / "secrets").mkdir(parents=True)
        (dirs.config / "installation.env").write_text("INSTALLATION_NAME=testinst\n")

    def decrypts():
        keys = tmp_path / "home" / ".config" / "sops" / "age" / "keys.txt"
        return keys.exists() and GOOD in keys.read_text()

    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="git@github.com:someone/media-server-engine.git\n"))
    commands.on(["git", "-C", str(dirs.engine), "remote", "set-url"])
    commands.on(["git", "clone"], done(then=cloned))
    commands.on(["bash", "-c"], real())
    commands.on(["bootstrap.sh"])
    commands.on(["age-keygen", "-y"], done(stdout="age1public\n"))
    commands.on(["engine-run", "setup-machine"])
    module = fresh_engine("engine.join")
    deployed = []
    monkeypatch.setattr(module.deploy_keys, "deploy", lambda *wanted: deployed.append(wanted))
    monkeypatch.setattr(module, "secrets_key_works", decrypts)
    module.deployed = deployed
    return module


def answering(monkeypatch, text):
    monkeypatch.setattr(sys, "stdin", io.StringIO(text))


def keys_file(tmp_path):
    return tmp_path / "home" / ".config" / "sops" / "age" / "keys.txt"


def existing_keys(tmp_path, text="AGE-SECRET-KEY-OTHER-INSTALLATION\n"):
    keys_file(tmp_path).parent.mkdir(parents=True)
    keys_file(tmp_path).write_text(text)


def test_joining_needs_an_installation_name(join, capsys):
    assert join.main([]) == 1
    assert capsys.readouterr().err == "join-installation: usage: make join-installation NAME=<installation name>\n"


def test_joining_refuses_an_installation_name_that_is_not_letters_digits_and_dashes(join, capsys):
    assert join.main(["Bad Name"]) == 1
    assert "lowercase letters, digits and dashes" in capsys.readouterr().err


def test_joining_sets_up_access_then_clones_and_hands_over_to_setup_machine(join, commands, dirs, monkeypatch):
    answering(monkeypatch, GOOD + "\n")
    assert join.main(["testinst"]) == 0
    assert join.deployed == [("someone/media-server-engine", "someone/media-server-config-testinst:write")]
    assert commands.did("git", "-C", str(dirs.engine), "remote", "set-url", "origin", "github-media-server-engine:someone/media-server-engine.git")
    assert commands.did("git", "clone", "github-media-server-config-testinst:someone/media-server-config-testinst.git", str(dirs.config))
    assert commands.index("bootstrap.sh") < commands.index("remote", "set-url") < commands.index("git", "clone") < commands.index("engine-run", "setup-machine")
    setup = next(command for command in commands.ran if command.args[1:] == ["setup-machine"])
    assert setup.env["RESTORE_FROM_BACKUP"] == "1"


def test_the_pasted_secrets_key_is_stored_privately(join, monkeypatch, tmp_path):
    answering(monkeypatch, GOOD + "\n")
    join.main(["testinst"])
    assert keys_file(tmp_path).read_text() == GOOD + "\n"
    assert os.stat(keys_file(tmp_path)).st_mode & 0o777 == 0o600


def test_an_existing_key_that_decrypts_the_config_is_not_asked_for_again(join, monkeypatch, tmp_path, capsys):
    existing_keys(tmp_path, GOOD + "\n")
    answering(monkeypatch, "")
    assert join.main(["testinst"]) == 0
    assert "Paste" not in capsys.readouterr().err


def test_spaces_and_a_carriage_return_pasted_around_the_key_are_dropped(join, monkeypatch, tmp_path):
    answering(monkeypatch, f"  {GOOD} \r\n")
    assert join.main(["testinst"]) == 0
    assert keys_file(tmp_path).read_text() == GOOD + "\n"


def test_something_that_is_not_an_age_key_is_refused_before_the_keys_file_is_touched(join, commands, monkeypatch, tmp_path, capsys):
    existing_keys(tmp_path)
    commands.on(["age-keygen", "-y"], done(stderr="age-keygen: error: failed to parse\n", returncode=1))
    answering(monkeypatch, "not a key at all\n")
    assert join.main(["testinst"]) == 1
    assert capsys.readouterr().err.endswith(
        "join-installation: that is not an age secrets key (the line starts with AGE-SECRET-KEY-); nothing was changed\n"
    )
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-OTHER-INSTALLATION\n"


def test_a_key_that_cannot_decrypt_the_config_leaves_the_keys_file_as_it_was_and_stops(join, commands, monkeypatch, tmp_path, capsys):
    existing_keys(tmp_path)
    answering(monkeypatch, "AGE-SECRET-KEY-WRONG\n")
    assert join.main(["testinst"]) == 1
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-OTHER-INSTALLATION\n"
    assert capsys.readouterr().err.endswith(
        "join-installation: that key cannot decrypt someone/media-server-config-testinst; check the password manager entry; the keys file is unchanged\n"
    )
    assert not commands.did("engine-run", "setup-machine")


def test_a_wrong_key_on_a_machine_without_keys_leaves_no_keys_file(join, monkeypatch, tmp_path):
    answering(monkeypatch, "AGE-SECRET-KEY-WRONG\n")
    join.main(["testinst"])
    assert not keys_file(tmp_path).exists()


def test_skip_restore_keeps_data_that_was_moved_into_place(join, commands, monkeypatch):
    os.environ["SKIP_RESTORE"] = "1"
    answering(monkeypatch, GOOD + "\n")
    join.main(["testinst"])
    setup = next(command for command in commands.ran if command.args[1:] == ["setup-machine"])
    assert "RESTORE_FROM_BACKUP" not in (setup.env or {})


def test_a_config_already_cloned_is_used_as_it_is(join, commands, dirs, monkeypatch):
    (dirs.config / ".git").mkdir(parents=True)
    (dirs.config / "installation.env").write_text("INSTALLATION_NAME=testinst\n")
    answering(monkeypatch, GOOD + "\n")
    join.main(["testinst"])
    assert not commands.did("git", "clone")


def test_run_from_an_engine_elsewhere_joining_moves_into_the_installation_first(join, monkeypatch):
    moved = []
    monkeypatch.setattr(join.installation, "move_into", lambda name, command: moved.append((name, command)))
    answering(monkeypatch, GOOD + "\n")
    join.main(["testinst"])
    assert moved == [("testinst", "join-installation")]


def test_secrets_key_works_asks_sops_to_decrypt_the_vpn_secrets(dirs, commands, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    module = fresh_engine("engine.join")
    commands.on(["sops", "decrypt", f"{dirs.config}/secrets/vpn.sops.env"], done(returncode=1), done())
    assert module.secrets_key_works() is False
    assert module.secrets_key_works() is True



def test_a_keys_file_without_a_last_newline_keeps_its_key_whole_when_a_key_is_added(join, monkeypatch, tmp_path):
    existing_keys(tmp_path, "AGE-SECRET-KEY-OTHER-INSTALLATION")
    answering(monkeypatch, GOOD + "\n")
    assert join.main(["testinst"]) == 0
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-OTHER-INSTALLATION\n" + GOOD + "\n"
