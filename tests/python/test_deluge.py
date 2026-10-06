import hashlib
import json
import os
import shutil
import stat
import subprocess
from types import SimpleNamespace

import pytest

from conftest import REPO, reset

PLUGIN_SOURCE = "b4af725398ecf4586cdb5d15c8637465de2d7614.tar.gz"
PLUGIN_SHA256 = "c400969cff22d7be00cbd7c55487380ad876f0c2a814b206866ef9c1ae62dedb"
CORE = {"enabled_plugins": [], "max_upload_speed": -1.0, "download_location": "/downloads"}


class Docker:
    def __init__(self, plugins_dir):
        self.plugins_dir = plugins_dir
        self.calls = []
        self.failing = {}

    def __call__(self, command, check, capture_output, text):
        assert command[0] == "docker"
        args = command[1:]
        self.calls.append(" ".join(args))
        for prefix, stderr in self.failing.items():
            if " ".join(args).startswith(prefix):
                raise subprocess.CalledProcessError(1, command, stderr=stderr)
        if args[:2] == ["exec", "deluge"] and args[2:4] == ["python3", "-c"]:
            return SimpleNamespace(stdout="3.12\n")
        if args[:4] == ["exec", "-u", "abc", "deluge"]:
            (self.plugins_dir / "AutoRemovePlus-2.0.0-py3.12.egg").touch()
        return SimpleNamespace(stdout="")

    def restarts(self):
        return [call for call in self.calls if call in ("stop deluge", "start deluge")]

    def builds(self):
        return [call for call in self.calls if call.startswith("exec -u abc deluge sh -c")]


@pytest.fixture
def deluge_config(dirs):
    config = dirs.data / "volumes" / "deluge" / "config"
    (config / "plugins").mkdir(parents=True)
    write_conf(config / "core.conf", {"file": 1, "format": 1}, CORE)
    return config


@pytest.fixture
def docker(deluge_config, monkeypatch):
    mock_docker = Docker(deluge_config / "plugins")
    monkeypatch.setattr(subprocess, "run", mock_docker)
    return mock_docker


@pytest.fixture
def web(http):
    http.on("POST", "/json", {"result": True, "error": None, "id": 1})
    return http


@pytest.fixture
def setup_deluge(wiring, dirs, monkeypatch):
    shutil.copy(REPO / "config-template" / "apps.yml", dirs.config / "apps.yml")
    monkeypatch.setenv("DELUGE_URL", "http://deluge")
    monkeypatch.setenv("DELUGE_READY_SECONDS", "0")

    def load(password="web pass", dry_run=False):
        return wiring("deluge", f"DELUGE_WEB_PASSWORD={password}\n", dry_run=dry_run)

    return load


@pytest.fixture
def deluge(setup_deluge):
    return setup_deluge()


def write_conf(path, header, body):
    path.write_text(json.dumps(header, indent=4) + json.dumps(body, indent=4))


def read_conf(path):
    text = path.read_text()
    decoder = json.JSONDecoder()
    header, end = decoder.raw_decode(text)
    body, _ = decoder.raw_decode(text[end:])
    return header, body


def password_matches(config, password):
    _, body = read_conf(config / "web.conf")
    return hashlib.sha1((body["pwd_salt"] + password).encode()).hexdigest() == body["pwd_sha1"]


def wire_once(setup_deluge, docker):
    setup_deluge().wire()
    docker.calls.clear()


def test_a_fresh_deluge_gets_its_plugins_settings_and_web_password_with_one_restart(deluge, deluge_config, docker, web, capsys):
    deluge.wire()
    _, core = read_conf(deluge_config / "core.conf")
    assert (core["enabled_plugins"], core["download_location"]) == (["Label", "AutoRemovePlus"], "/data/downloads")
    assert password_matches(deluge_config, "web pass")
    header, body = read_conf(deluge_config / "web.conf")
    assert (header, body["first_login"]) == ({"file": 2, "format": 1}, False)
    _, remove = read_conf(deluge_config / "autoremoveplus.conf")
    assert (remove["min"], remove["sel_func"], remove["remove_data"]) == (168.0, "or", True)
    for name in ("web.conf", "autoremoveplus.conf"):
        assert stat.S_IMODE(os.stat(deluge_config / name).st_mode) == 0o600
    assert docker.restarts() == ["stop deluge", "start deluge"]
    reported = capsys.readouterr().out
    assert "deluge: enable plugin AutoRemovePlus" in reported
    assert "deluge: set web password" in reported


def test_the_wiring_ends_by_signing_in_to_the_web_ui_with_the_password(deluge, docker, web):
    deluge.wire()
    assert web.body("POST", "/json") == {"method": "auth.login", "params": ["web pass"], "id": 1}


def test_a_missing_plugin_egg_is_built_in_the_container_from_the_pinned_source_before_the_restart(deluge, docker, web):
    deluge.wire()
    [build] = docker.builds()
    assert PLUGIN_SOURCE in build and PLUGIN_SHA256 in build and "sha256sum -c" in build
    assert docker.calls.index(build) < docker.calls.index("stop deluge")


def test_an_egg_built_for_the_containers_python_is_not_built_again(deluge, deluge_config, docker, web):
    (deluge_config / "plugins" / "AutoRemovePlus-2.0.0-py3.12.egg").touch()
    deluge.wire()
    assert docker.builds() == []


def test_an_already_wired_deluge_is_left_untouched(setup_deluge, deluge_config, docker, web, capsys):
    wire_once(setup_deluge, docker)
    web_conf = (deluge_config / "web.conf").read_text()
    capsys.readouterr()
    setup_deluge().wire()
    assert docker.restarts() == [] and docker.builds() == []
    assert (deluge_config / "web.conf").read_text() == web_conf
    assert capsys.readouterr().out == ""


def test_a_drifted_core_setting_is_corrected_and_the_rest_of_core_conf_is_kept(setup_deluge, deluge_config, docker, web, dirs, capsys):
    (dirs.config / "apps.yml").write_text("deluge:\n  core:\n    max_upload_speed: 2000.0\n  plugins:\n    - name: Label\n")
    wire_once(setup_deluge, docker)
    header, core = read_conf(deluge_config / "core.conf")
    write_conf(deluge_config / "core.conf", header, dict(core, max_upload_speed=500.0))
    setup_deluge().wire()
    _, core = read_conf(deluge_config / "core.conf")
    assert (core["max_upload_speed"], core["download_location"]) == (2000.0, "/downloads")
    assert docker.restarts() == ["stop deluge", "start deluge"]
    assert "deluge: set max_upload_speed 500.0 -> 2000.0" in capsys.readouterr().out


def test_a_changed_web_password_is_applied_without_knowing_the_old_one(setup_deluge, deluge_config, docker, web):
    wire_once(setup_deluge, docker)
    setup_deluge(password="new pass").wire()
    assert password_matches(deluge_config, "new pass")


def test_plugins_enabled_by_hand_are_kept(deluge, deluge_config, docker, web):
    write_conf(deluge_config / "core.conf", {"file": 1, "format": 1}, dict(CORE, enabled_plugins=["Scheduler"]))
    deluge.wire()
    _, core = read_conf(deluge_config / "core.conf")
    assert core["enabled_plugins"] == ["Scheduler", "Label", "AutoRemovePlus"]


def test_a_dry_run_reports_the_changes_and_touches_nothing(setup_deluge, deluge_config, docker, web, capsys):
    core_before = (deluge_config / "core.conf").read_text()
    setup_deluge(dry_run=True).wire()
    reported = capsys.readouterr().out
    assert "(dry run) deluge: enable plugin Label" in reported
    assert "(dry run) deluge: build plugin AutoRemovePlus for python 3.12" in reported
    assert (deluge_config / "core.conf").read_text() == core_before
    assert not (deluge_config / "web.conf").exists()
    assert docker.restarts() == [] and docker.builds() == []
    assert web.requests == []


def test_a_failed_plugin_build_fails_the_step_after_the_settings_are_applied(deluge, deluge_config, docker, web):
    docker.failing["exec -u abc deluge sh -c"] = "sha256sum: WARNING: 1 computed checksum did NOT match"
    with pytest.raises(deluge.WiringError, match="could not build plugin AutoRemovePlus: docker exec deluge failed: sha256sum: WARNING"):
        deluge.wire()
    assert password_matches(deluge_config, "web pass")


def test_a_web_ui_that_never_accepts_the_password_fails_the_step(deluge, docker, http):
    http.on("POST", "/json", {"result": False, "error": None, "id": 1})
    with pytest.raises(deluge.WiringError, match="the web UI does not accept the web password"):
        deluge.wire()


def test_a_web_ui_that_drops_connections_while_starting_is_waited_for(deluge, docker, http, monkeypatch):
    monkeypatch.setenv("DELUGE_READY_SECONDS", "5")
    monkeypatch.setattr(deluge.time, "sleep", lambda seconds: None)
    http.on("POST", "/json", reset(), reset(), {"result": True, "error": None, "id": 1})
    deluge.wire()
    assert len(http.requests) == 3


def test_a_docker_command_that_fails_ends_the_step_with_dockers_error(deluge, docker, web):
    docker.failing["stop deluge"] = "Error response from daemon: No such container: deluge"
    with pytest.raises(deluge.WiringError, match="docker stop deluge failed: Error response from daemon: No such container: deluge"):
        deluge.wire()


def test_a_config_deluge_cannot_have_written_is_reported_and_deluge_is_still_started_again(deluge, docker, web, monkeypatch):
    def full_disk(conf):
        raise OSError(28, "No space left on device")

    monkeypatch.setattr(deluge.DelugeConfig, "save", full_disk)
    with pytest.raises(deluge.WiringError, match=r"^could not write core.conf: No space left on device$"):
        deluge.wire()
    assert docker.restarts() == ["stop deluge", "start deluge"]


def test_a_failed_write_is_still_reported_when_deluge_will_not_start_again_either(deluge, docker, web, monkeypatch):
    def full_disk(conf):
        raise OSError(28, "No space left on device")

    monkeypatch.setattr(deluge.DelugeConfig, "save", full_disk)
    docker.failing["start deluge"] = "Error response from daemon: No such container: deluge"
    with pytest.raises(deluge.WiringError, match="could not write core.conf: No space left on device; docker start deluge failed: .*No such container"):
        deluge.wire()
