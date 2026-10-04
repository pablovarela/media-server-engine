import io
import os
import shutil
import signal
import stat
import sys

import pytest

from conftest import REPO, done, fresh_engine

KEYGEN = "# created: now\n# public key: age1newpublic\nAGE-SECRET-KEY-NEW\n"


@pytest.fixture
def create(dirs, commands, tmp_path, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    install = dirs.engine.parent
    os.environ.update(HOME=str(tmp_path / "home"), INSTALL_DIR=str(install))
    for name in ("SOPS_AGE_KEY_FILE", "GITHUB_OWNER", "CREATED_INSTALL_DIR"):
        os.environ.pop(name, None)
    os.rmdir(dirs.config)
    os.rmdir(dirs.data)
    shutil.copytree(REPO / "config-template", dirs.engine / "config-template")
    shutil.copytree(REPO / "homepage", dirs.engine / "homepage")

    def keygen():
        args = commands.ran[-1].args
        with open(args[args.index("-o") + 1], "w") as key:
            key.write(KEYGEN)

    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="git@github.com:someone/media-server-engine.git\n"))
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--exact-match"], done(returncode=128))
    commands.on(["git", "-C", str(dirs.config), "init"], done(then=lambda: (dirs.config / ".git").mkdir()))
    commands.on(["gh", "auth", "status"])
    commands.on(["gh", "repo", "view"], done(returncode=1))
    commands.on(["age-keygen", "-o"], done(then=keygen))
    commands.on(["age-keygen", "-y"], done(stdout="age1other\n"))
    commands.on(["bootstrap.sh"])
    commands.on(["configure.sh"])
    commands.on(["engine-run", "setup-machine"])
    monkeypatch.setattr(sys, "stdin", io.StringIO("\n"))
    return fresh_engine("engine.create")


def keys_file(tmp_path):
    return tmp_path / "home" / ".config" / "sops" / "age" / "keys.txt"


def existing_keys(tmp_path):
    keys_file(tmp_path).parent.mkdir(parents=True)
    keys_file(tmp_path).write_text("AGE-SECRET-KEY-EXISTING\n")


def mode(path):
    return stat.S_IMODE(os.stat(path).st_mode)


def test_creating_needs_an_installation_name(create, capsys):
    assert create.main([]) == 1
    assert capsys.readouterr().err == "create-installation: usage: make create-installation NAME=<installation name>\n"


def test_the_name_can_come_from_the_environment(create, commands):
    os.environ["NAME"] = "testinst"
    assert create.main([]) == 0
    assert commands.did("configure.sh")


def test_an_installation_name_that_is_not_letters_digits_and_dashes_is_refused_before_anything_is_made(create, commands, capsys):
    assert create.main(["trialpub=age1x"]) == 1
    assert "lowercase letters, digits and dashes" in capsys.readouterr().err
    assert commands.ran == []


def test_creating_refuses_an_installation_whose_config_repo_exists_on_github(create, commands, capsys):
    commands.on(["gh", "repo", "view", "someone/media-server-config-testinst"])
    assert create.main(["testinst"]) == 1
    assert capsys.readouterr().err == "create-installation: someone/media-server-config-testinst already exists; use make join-installation NAME=testinst\n"
    assert not commands.did("age-keygen")


def test_creating_refuses_a_config_folder_that_is_not_empty(create, dirs, commands, capsys):
    dirs.config.mkdir()
    (dirs.config / "stray").touch()
    assert create.main(["testinst"]) == 1
    assert capsys.readouterr().err == f"create-installation: {dirs.config} is not empty\n"


def test_creating_works_without_the_github_cli_for_a_local_only_config(create, commands):
    commands.on(["gh", "auth", "status"], done(returncode=1))
    assert create.main(["testinst"]) == 0
    assert commands.did("configure.sh")


def test_a_new_secrets_key_is_added_next_to_existing_ones_and_shown_once(create, tmp_path, capsys):
    existing_keys(tmp_path)
    assert create.main(["testinst"]) == 0
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-EXISTING\nAGE-SECRET-KEY-NEW\n"
    assert mode(keys_file(tmp_path)) == 0o600
    assert mode(keys_file(tmp_path).parent) == 0o700
    err = capsys.readouterr().err
    assert err.count("AGE-SECRET-KEY-NEW") == 1
    assert (
        "The secrets key for testinst. Save this line in your password manager now; without it nothing in testinst's config can be decrypted:\n\n"
        "AGE-SECRET-KEY-NEW\n\nPress Enter once it is saved. "
    ) in err


def test_the_config_starts_from_the_template_with_this_engines_path_filled_in(create, dirs):
    create.main(["testinst"])
    assert (dirs.config / "images.yml").exists()
    assert '"depNameTemplate": "someone/media-server-engine"' in (dirs.config / "renovate.json").read_text()
    for folder, _, files in os.walk(dirs.config):
        for name in files:
            assert b"ENGINE_REPOSITORY" not in (open(os.path.join(folder, name), "rb").read()), name
    assert (dirs.config / ".sops.yaml").read_text() == "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: age1newpublic\n"


def test_a_new_config_starts_with_a_copy_of_the_engines_landing_page(create, dirs):
    create.main(["testinst"])
    for page in ("settings.yaml", "services.yaml", "widgets.yaml", "bookmarks.yaml", "custom.css"):
        assert (dirs.config / "homepage" / page).read_bytes() == (REPO / "homepage" / page).read_bytes()


def test_an_engine_between_releases_is_used_as_it_is(create, dirs):
    create.main(["testinst"])
    assert (dirs.config / "engine.env").read_text() == "ENGINE_VERSION=local\n"


def test_an_engine_on_a_release_pins_that_release(create, dirs, commands):
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--exact-match"], done(stdout="v1.2.0\n"))
    create.main(["testinst"])
    assert (dirs.config / "engine.env").read_text() == "ENGINE_VERSION=v1.2.0\n"


def test_creating_configures_then_sets_up_this_machine_as_a_new_installation(create, commands):
    assert create.main(["testinst"]) == 0
    configure = next(command for command in commands.ran if command.args[0].endswith("configure.sh"))
    assert (configure.env["NAME"], configure.env["CONFIGURE_FROM_CREATE"]) == ("testinst", "1")
    assert commands.index("bootstrap.sh") < commands.index("age-keygen", "-o") < commands.index("git", "init") < commands.index("configure.sh") < commands.index("engine-run", "setup-machine")
    assert not commands.did("gh", "repo", "create")


def undo_answers(commands):
    commands.on(["age-keygen", "-y"], done(stdout="age1existing\n"), done(stdout="age1newpublic\n"))


def test_a_create_stopped_before_the_settings_are_saved_leaves_nothing_behind(create, commands, dirs, tmp_path, capsys):
    existing_keys(tmp_path)
    undo_answers(commands)
    inode = os.stat(keys_file(tmp_path)).st_ino
    commands.on(["configure.sh"], done(returncode=1))
    assert create.main(["testinst"]) == 1
    assert not dirs.config.exists() and not dirs.data.exists()
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-EXISTING\n"
    assert os.stat(keys_file(tmp_path)).st_ino != inode
    assert mode(keys_file(tmp_path)) == 0o600
    assert os.listdir(keys_file(tmp_path).parent) == ["keys.txt"]
    err = capsys.readouterr().err
    assert "The secrets key shown for testinst was removed and is no longer needed; delete it from your password manager.\n" in err
    assert err.endswith("Stopped before testinst's settings were saved, so nothing was kept. Run make create-installation NAME=testinst again.\n")


def test_a_data_folder_that_was_already_there_is_kept_when_create_stops(create, commands, dirs):
    dirs.data.mkdir()
    (dirs.data / "volumes").mkdir()
    commands.on(["age-keygen", "-y"], done(stdout="age1newpublic\n"))
    commands.on(["configure.sh"], done(returncode=1))
    create.main(["testinst"])
    assert (dirs.data / "volumes").is_dir()


def test_a_stopped_create_that_copied_the_engine_removes_the_copy_too(create, commands, dirs, tmp_path):
    os.environ["CREATED_INSTALL_DIR"] = str(dirs.engine.parent)
    commands.on(["age-keygen", "-y"], done(stdout="age1newpublic\n"))
    commands.on(["configure.sh"], done(returncode=1))
    create.main(["testinst"])
    assert not dirs.engine.parent.exists()


@pytest.mark.parametrize("stop", ["interrupt", "terminate"])
def test_stopping_create_during_the_questions_also_leaves_nothing_behind(create, commands, dirs, tmp_path, stop):
    previous = signal.signal(signal.SIGTERM, create.program.stop_on_terminate)
    commands.on(["age-keygen", "-y"], done(stdout="age1newpublic\n"))

    def stopping():
        if stop == "interrupt":
            raise KeyboardInterrupt
        os.kill(os.getpid(), signal.SIGTERM)

    commands.on(["configure.sh"], done(then=stopping))
    try:
        if stop == "interrupt":
            assert create.main(["testinst"]) == 130
        else:
            with pytest.raises(SystemExit) as ended:
                create.main(["testinst"])
            assert ended.value.code == 143
    finally:
        signal.signal(signal.SIGTERM, previous)
    assert not dirs.config.exists()
    assert "AGE-SECRET-KEY-NEW" not in (keys_file(tmp_path).read_text() if keys_file(tmp_path).exists() else "")


def test_a_create_that_fails_after_the_settings_are_saved_keeps_them_and_says_how_to_finish(create, commands, dirs, tmp_path, capsys):
    commands.on(["engine-run", "setup-machine"], done(returncode=1))
    assert create.main(["testinst"]) == 1
    assert (dirs.config / "images.yml").exists()
    assert "AGE-SECRET-KEY-NEW" in keys_file(tmp_path).read_text()
    assert capsys.readouterr().err.endswith(
        f"testinst's settings are saved in {dirs.config}. Finish setting up this machine with: cd {dirs.engine.parent} && make setup-machine\n"
    )


def test_run_from_an_engine_elsewhere_creating_moves_into_the_installation_first(create, commands, monkeypatch):
    moved = []
    monkeypatch.setattr(create.installation, "move_into", lambda name, command: moved.append((name, command)))
    create.main(["testinst"])
    assert moved == [("testinst", "create-installation")]



def test_stopping_at_the_save_it_prompt_removes_the_new_key(create, commands, monkeypatch, tmp_path, capsys):
    existing_keys(tmp_path)
    commands.on(["age-keygen", "-y"], done(stdout="age1existing\n"), done(stdout="age1newpublic\n"))

    def interrupted():
        raise KeyboardInterrupt

    monkeypatch.setattr(create.prompt, "line", interrupted)
    assert create.main(["testinst"]) == 130
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-EXISTING\n"
    assert "was removed and is no longer needed" in capsys.readouterr().err


def test_a_keys_file_without_a_last_newline_keeps_its_key_whole(create, tmp_path):
    keys_file(tmp_path).parent.mkdir(parents=True)
    keys_file(tmp_path).write_text("AGE-SECRET-KEY-EXISTING")
    assert create.main(["testinst"]) == 0
    assert keys_file(tmp_path).read_text() == "AGE-SECRET-KEY-EXISTING\nAGE-SECRET-KEY-NEW\n"



def test_an_error_inside_create_is_shown_before_the_undo_messages(create, commands, dirs, capsys):
    shutil.rmtree(dirs.engine / "config-template")
    commands.on(["age-keygen", "-y"], done(stdout="age1newpublic\n"))
    assert create.main(["testinst"]) == 1
    err = capsys.readouterr().err
    assert err.index("create-installation: [Errno 2]") < err.index("Stopped before testinst's settings were saved")


def test_undoing_keeps_the_other_keys_byte_for_byte(create, commands, tmp_path):
    keys_file(tmp_path).parent.mkdir(parents=True)
    keys_file(tmp_path).write_bytes(b"AGE-SECRET-KEY-EXISTING\r\n")
    commands.on(["age-keygen", "-y"], done(stdout="age1existing\n"), done(stdout="age1newpublic\n"))
    commands.on(["configure.sh"], done(returncode=1))
    create.main(["testinst"])
    assert keys_file(tmp_path).read_bytes() == b"AGE-SECRET-KEY-EXISTING\r\n"
