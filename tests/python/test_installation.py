import os
import platform
import shutil
import socket
import sys

from types import SimpleNamespace
from unittest import mock

import pytest

from conftest import REPO, done, fresh_engine


@pytest.fixture
def installation(dirs, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    return fresh_engine("engine.installation")


def test_the_directories_default_to_siblings_of_the_engine(monkeypatch):
    monkeypatch.setattr(os, "environ", {"ENGINE_DIR": "/home/me/home/engine"})
    fresh_engine("engine.installation").export_directories()
    assert os.environ["CONFIG_DIR"] == "/home/me/home/config"
    assert os.environ["DATA_DIR"] == "/home/me/home/data"


def test_the_engine_directory_defaults_to_this_checkout(monkeypatch):
    monkeypatch.setattr(os, "environ", {})
    fresh_engine("engine.installation").export_directories()
    assert os.environ["ENGINE_DIR"] == str(REPO)


def test_loading_the_installation_reads_values_as_bash_would(installation, dirs, monkeypatch):
    monkeypatch.setitem(os.environ, "HOME", "/home/me")
    (dirs.config / "installation.env").write_text(
        "# set by make configure\nINSTALLATION_NAME=home\n\nTZ=\"Europe/London\"\nMEDIA_SERVER_HOST='media.local' # the Pi\n"
        "export HOMEPAGE_PORT=8080\nRESTIC_REPOSITORY=~/backups\nHOMEPAGE_ALLOWED_HOSTS=\"$HOME/x\"\nJELLYFIN_ADMIN_USER= # unset\n"
    )
    installation.load_installation()
    assert {name: os.environ[name] for name in (
        "INSTALLATION_NAME", "TZ", "MEDIA_SERVER_HOST", "HOMEPAGE_PORT", "RESTIC_REPOSITORY", "HOMEPAGE_ALLOWED_HOSTS", "JELLYFIN_ADMIN_USER",
    )} == {
        "INSTALLATION_NAME": "home",
        "TZ": "Europe/London",
        "MEDIA_SERVER_HOST": "media.local",
        "HOMEPAGE_PORT": "8080",
        "RESTIC_REPOSITORY": "/home/me/backups",
        "HOMEPAGE_ALLOWED_HOSTS": "/home/me/x",
        "JELLYFIN_ADMIN_USER": "",
    }


def test_loading_leaves_the_shells_own_variables_out(installation, dirs, monkeypatch):
    monkeypatch.setitem(os.environ, "PWD", "/somewhere")
    os.environ.pop("SHLVL", None)
    (dirs.config / "installation.env").write_text("INSTALLATION_NAME=home\n")
    installation.load_installation()
    assert os.environ["PWD"] == "/somewhere"
    assert "SHLVL" not in os.environ


def test_an_installation_file_bash_cannot_read_stops_the_update(installation, dirs, capfd):
    (dirs.config / "installation.env").write_text("INSTALLATION_NAME=home\nJELLYFIN_ADMIN_USER=O'Brien\n")
    with pytest.raises(installation.commands.CommandFailed):
        installation.load_installation()
    assert "installation.env: line 2" in capfd.readouterr().err


def test_an_installation_without_a_name_is_refused(installation, dirs):
    (dirs.config / "installation.env").write_text("TZ=Europe/London\n")
    with pytest.raises(installation.commands.Stop) as stop:
        installation.load_installation()
    assert str(stop.value) == f"INSTALLATION_NAME is not set in {dirs.config}/installation.env"


def test_a_directory_without_a_config_is_not_an_installation_and_says_which_are(installation, dirs, tmp_path, monkeypatch):
    home = tmp_path / "home"
    (home / "other" / "config").mkdir(parents=True)
    (home / "other" / "engine").mkdir()
    (home / "other" / "config" / "installation.env").write_text("INSTALLATION_NAME=other\n")
    monkeypatch.setitem(os.environ, "HOME", str(home))
    with pytest.raises(installation.commands.Stop) as stop:
        installation.load_installation()
    assert stop.value.text("update") == (
        f"{dirs.engine} is not an installation: there is no config next to it.\n"
        "Installations on this machine; run make from the one you mean:\n"
        f"  cd {home}/other\n"
        "To create one: make create-installation NAME=<name>"
    )


def test_an_installation_gets_the_engines_makefile_at_its_root(installation, dirs):
    (dirs.engine / "installation").mkdir()
    shutil.copy(REPO / "installation" / "Makefile", dirs.engine / "installation" / "Makefile")
    installation.write_installation_makefile()
    assert (dirs.engine.parent / "Makefile").read_text() == (REPO / "installation" / "Makefile").read_text()


def test_an_engine_not_named_engine_is_not_inside_an_installation(installation, tmp_path, monkeypatch):
    monkeypatch.setitem(os.environ, "ENGINE_DIR", str(tmp_path / "checkout"))
    assert installation.installation_root() is None


def test_the_main_is_the_machine_holding_the_marker(installation, dirs):
    assert installation.machine_role() == "secondary"
    (dirs.data / ".backup-main").touch()
    assert installation.machine_role() == "main"


def test_the_network_name_is_the_configured_host_or_this_machines_local_name(installation, monkeypatch, commands):
    monkeypatch.setitem(os.environ, "MEDIA_SERVER_HOST", "media.example")
    assert installation.network_name() == "media.example"
    del os.environ["MEDIA_SERVER_HOST"]
    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(socket, "gethostname", lambda: "gorgon.lan")
    assert installation.network_name() == "gorgon.local"
    monkeypatch.setattr(platform, "system", lambda: "Darwin")
    commands.on(["scutil", "--get", "LocalHostName"], done(stdout="laptop\n"))
    assert installation.network_name() == "laptop.local"


def test_systemd_runs_when_its_runtime_directory_and_systemctl_exist(installation, tmp_path, monkeypatch):
    monkeypatch.setitem(os.environ, "SYSTEMD_RUNTIME_DIR", str(tmp_path))
    monkeypatch.setattr(shutil, "which", lambda name: "/usr/bin/systemctl")
    assert installation.systemd_running() is True
    monkeypatch.setattr(shutil, "which", lambda name: None)
    assert installation.systemd_running() is False


def test_the_landing_page_port_defaults_to_80(installation, monkeypatch):
    assert installation.homepage_port() == "80"
    monkeypatch.setitem(os.environ, "HOMEPAGE_PORT", "8080")
    assert installation.homepage_port() == "8080"


def engine_git(commands, dirs, tag, remote):
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--always"], done(stdout=f"{tag}\n"))
    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout=f"{remote}\n"))
    commands.on(["git", "-C", str(dirs.engine), "rev-parse", "HEAD"], done(stdout="abc123\n"))


def test_a_release_links_to_its_page_on_github(installation, dirs, commands):
    engine_git(commands, dirs, "v1.0.0", "github-media-server-engine:someone/media-server-engine.git")
    assert installation.engine_page_url() == "https://github.com/someone/media-server-engine/releases/tag/v1.0.0"


def test_between_releases_the_link_is_to_the_commit(installation, dirs, commands):
    engine_git(commands, dirs, "v1.0.0-3-gabc1234", "https://github.com/someone/media-server-engine.git")
    assert installation.engine_page_url() == "https://github.com/someone/media-server-engine/commit/abc123"


def test_an_engine_not_on_github_has_no_link(installation, dirs, commands):
    engine_git(commands, dirs, "v1.0.0", "git@example.com:someone/engine.git")
    assert installation.engine_page_url() == ""


def test_without_other_installations_the_message_only_says_how_to_create_one(installation, dirs, tmp_path, monkeypatch):
    monkeypatch.setitem(os.environ, "HOME", str(tmp_path / "empty-home"))
    with pytest.raises(installation.commands.Stop) as stop:
        installation.load_installation()
    assert stop.value.text("update") == (
        f"{dirs.engine} is not an installation: there is no config next to it.\n"
        "To create one: make create-installation NAME=<name>"
    )


def test_an_engine_checkout_outside_an_installation_gets_no_makefile(installation, tmp_path, monkeypatch):
    monkeypatch.setitem(os.environ, "ENGINE_DIR", str(tmp_path / "checkout"))
    installation.write_installation_makefile()
    assert not (tmp_path / "Makefile").exists()


def test_an_outdated_installation_makefile_is_replaced(installation, dirs):
    (dirs.engine / "installation").mkdir()
    shutil.copy(REPO / "installation" / "Makefile", dirs.engine / "installation" / "Makefile")
    (dirs.engine.parent / "Makefile").write_text("old\n")
    installation.write_installation_makefile()
    assert (dirs.engine.parent / "Makefile").read_text() == (REPO / "installation" / "Makefile").read_text()


@pytest.mark.parametrize("name", ["home", "media-1", "a" * 40])
def test_installation_names_are_lowercase_letters_digits_and_dashes(installation, name):
    installation.require_valid_name(name)


@pytest.mark.parametrize("name", ["Bad Name", "Trial", "trialpub=age1x", "-dash", "a/b", "a" * 41, ""])
def test_other_installation_names_are_refused(installation, name):
    with pytest.raises(installation.commands.Stop) as stop:
        installation.require_valid_name(name)
    assert str(stop.value) == (
        "installation names are lowercase letters, digits and dashes, up to 40 characters, "
        f"starting with a letter or digit; got '{name}'"
    )


class Restarted(Exception):
    pass


@pytest.fixture
def moving(installation, commands, tmp_path, monkeypatch):
    source = tmp_path / "checkout" / "media-server-engine"
    source.mkdir(parents=True)
    monkeypatch.setitem(os.environ, "ENGINE_DIR", str(source))
    monkeypatch.setitem(os.environ, "HOME", str(tmp_path / "home"))
    os.environ.pop("INSTALL_DIR", None)
    commands.on(["git", "clone", "-q"])
    commands.on(["git", "-C", str(source), "symbolic-ref", "-q", "HEAD"])
    commands.on(["git", "-C", str(source), "remote", "get-url", "origin"], done(stdout="git@github.com:someone/media-server-engine.git\n"))
    commands.on(["git", "-C", str(source), "rev-parse", "HEAD"], done(stdout="abc123\n"))
    commands.on(["remote", "set-url"])
    commands.on(["checkout", "-q"])
    monkeypatch.setattr(os, "execv", mock.Mock(side_effect=Restarted))
    return SimpleNamespace(source=source, home=tmp_path / "home")


def test_an_engine_elsewhere_is_copied_into_the_installation_and_takes_over(installation, moving, commands, capsys):
    with pytest.raises(Restarted):
        installation.move_into("newinst", "create-installation")
    target = moving.home / "newinst"
    assert commands.did("git", "clone", "-q", str(moving.source), str(target / "engine"))
    assert commands.did("git", "-C", str(target / "engine"), "remote", "set-url", "origin", "git@github.com:someone/media-server-engine.git")
    assert not commands.did("checkout", "-q")
    program = str(target / "engine" / "scripts" / "engine-run")
    os.execv.assert_called_once_with(program, [program, "create-installation", "newinst"])
    assert (os.environ["ENGINE_DIR"], os.environ["CONFIG_DIR"], os.environ["DATA_DIR"], os.environ["INSTALL_DIR"], os.environ["CREATED_INSTALL_DIR"]) == (
        str(target / "engine"), str(target / "config"), str(target / "data"), str(target), str(target),
    )
    assert capsys.readouterr().err == f"Installing newinst in {target}: engine, config and data side by side.\n"


def test_an_engine_copied_from_a_detached_release_checks_out_that_commit(installation, moving, commands):
    commands.on(["git", "-C", str(moving.source), "symbolic-ref", "-q", "HEAD"], done(returncode=1))
    with pytest.raises(Restarted):
        installation.move_into("newinst", "join-installation")
    assert commands.did("git", "-C", str(moving.home / "newinst" / "engine"), "checkout", "-q", "abc123")


def test_an_existing_installation_folder_is_not_marked_as_made_by_this_run(installation, moving, tmp_path):
    os.environ["INSTALL_DIR"] = str(tmp_path / "existing")
    (tmp_path / "existing").mkdir()
    with pytest.raises(Restarted):
        installation.move_into("newinst", "create-installation")
    assert os.environ["CREATED_INSTALL_DIR"] == ""


def test_an_installation_folder_that_already_has_an_engine_is_not_overwritten(installation, moving, commands):
    (moving.home / "newinst" / "engine").mkdir(parents=True)
    with pytest.raises(installation.commands.Stop) as stop:
        installation.move_into("newinst", "create-installation")
    engine = moving.home / "newinst" / "engine"
    assert str(stop.value) == f"{engine} already exists; run make from {engine} instead"
    assert not commands.did("git", "clone")


def test_an_engine_already_in_its_installation_stays(installation, moving, commands, monkeypatch):
    engine = moving.home / "newinst" / "engine"
    engine.mkdir(parents=True)
    monkeypatch.setitem(os.environ, "ENGINE_DIR", str(engine))
    installation.move_into("newinst", "create-installation")
    assert commands.ran == []
