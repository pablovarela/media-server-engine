import grp
import os
import pwd
import shutil

import pytest

from conftest import REPO, done, fresh_engine


@pytest.fixture
def timers(installed, commands, tmp_path, monkeypatch):
    (installed.engine / "systemd").mkdir()
    for unit in (REPO / "systemd").iterdir():
        shutil.copy(unit, installed.engine / "systemd" / unit.name)
    os.environ.update(UNIT_DIR=str(tmp_path / "units"), SYSTEMD_RUNTIME_DIR=str(tmp_path), HOME="/home/me")
    monkeypatch.setattr(shutil, "which", lambda name: f"/usr/local/bin/{name}")
    commands.on(["sudo", "tee"])
    commands.on(["sudo", "systemctl"])
    module = fresh_engine("engine.timers")
    monkeypatch.setattr(module.role, "is_main", lambda: 0)
    return module


def written(commands, tmp_path, name):
    path = str(tmp_path / "units" / name)
    return next(command.process.input for command in commands.ran if command.args == ["sudo", "tee", path])


def test_units_are_rendered_for_this_checkout_and_user(timers, commands, installed, tmp_path):
    assert timers.main(["media-backup", "media-verify"]) == 0
    service = written(commands, tmp_path, "media-backup.service")
    assert f"WorkingDirectory={installed.engine}\n" in service
    assert f"Environment=CONFIG_DIR={installed.config}\n" in service
    assert f"Environment=DATA_DIR={installed.data}\n" in service
    assert f"User={pwd.getpwuid(os.geteuid()).pw_name}\n" in service
    assert f"Group={grp.getgrgid(os.getegid()).gr_name}\n" in service
    assert f"ExecStart=/usr/local/bin/sops exec-env {installed.config}/secrets/healthchecks.sops.env '/usr/local/bin/sops exec-env {installed.config}/secrets/backup.sops.env scripts/engine-run backup'\n" in service
    assert "Environment=SOPS_AGE_KEY_FILE=/home/me/.config/sops/age/keys.txt\n" in written(commands, tmp_path, "media-verify.service")
    assert "@" not in service


def test_the_timers_are_reloaded_and_enabled(timers, commands):
    timers.main(["media-backup", "media-verify"])
    assert commands.did("sudo", "systemctl", "daemon-reload")
    assert commands.ran[-1].args == ["sudo", "systemctl", "enable", "--now", "media-backup.timer", "media-verify.timer"]


def test_only_the_timers_given_are_installed(timers, commands, installed, tmp_path):
    assert timers.main(["media-download-cleanup"]) == 0
    assert [command.args[2] for command in commands.ran if command.args[:2] == ["sudo", "tee"]] == [
        str(tmp_path / "units" / "media-download-cleanup.service"), str(tmp_path / "units" / "media-download-cleanup.timer"),
    ]
    assert f"ExecStart={installed.engine}/scripts/engine-run remove-executable-downloads\n" in written(commands, tmp_path, "media-download-cleanup.service")
    assert commands.ran[-1].args == ["sudo", "systemctl", "enable", "--now", "media-download-cleanup.timer"]


def test_every_unit_renders_with_no_placeholder_left(timers, commands):
    names = sorted({path.stem for path in (REPO / "systemd").iterdir()})
    assert timers.main(names) == 0
    rendered = [command.process.input for command in commands.ran if command.args[:2] == ["sudo", "tee"]]
    assert len(rendered) == len(list((REPO / "systemd").iterdir()))
    assert not any("@" in text and any(f"@{word}@" in text for word in ("ENGINE_DIR", "CONFIG_DIR", "DATA_DIR", "USER", "GROUP", "HOME", "SOPS")) for text in rendered)


def test_relative_config_and_data_paths_are_written_normalized(timers, commands, installed, tmp_path):
    os.environ.update(CONFIG_DIR=f"{installed.engine}/../config", DATA_DIR=f"{installed.engine}/../data-rel")
    assert timers.main(["media-backup"]) == 0
    service = written(commands, tmp_path, "media-backup.service")
    assert "/../" not in service
    assert f"Environment=DATA_DIR={installed.engine.parent}/data-rel\n" in service


def test_installing_needs_unit_names(timers, commands, capsys):
    assert timers.main([]) == 1
    assert capsys.readouterr().err == "install-timers: usage: install-timers UNIT_NAME..., for example media-backup\n"
    assert not commands.did("sudo")


def test_installing_needs_systemd(timers, commands, monkeypatch, capsys):
    monkeypatch.setattr(shutil, "which", lambda name: None)
    assert timers.main(["media-backup"]) == 1
    assert "timers need systemd, and systemd is not running on this machine" in capsys.readouterr().err
    assert not commands.did("sudo")


def test_backup_timers_are_refused_on_a_machine_that_is_not_the_main(timers, commands, monkeypatch, capsys):
    monkeypatch.setattr(timers.role, "is_main", lambda: 1)
    assert timers.main(["media-backup", "media-verify"]) == 1
    assert capsys.readouterr().err == "install-timers: this machine is not testinst's main; run make claim-backup-main first\n"
    assert not commands.did("sudo")


def test_installing_the_backup_timers_marks_the_machine_as_the_main(timers, installed):
    timers.main(["media-backup", "media-verify"])
    assert (installed.data / ".backup-main").exists()


def test_other_timers_install_on_any_machine(timers, installed, monkeypatch):
    monkeypatch.setattr(timers.role, "is_main", lambda: 1)
    assert timers.main(["media-update", "media-download-cleanup"]) == 0
    assert not (installed.data / ".backup-main").exists()
