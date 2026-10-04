import json
import os
import shutil
import subprocess

import pytest

from conftest import REPO, done, fresh_engine, real

PINNED = "@sha256:" + "0" * 64


@pytest.fixture
def stack(installed, commands):
    return fresh_engine("engine.stack")


def merged(commands, stack_services, monitoring_services=None):
    commands.on(["docker", "compose", "--project-name", "media-server", "config", "--format", "json"], done(stdout=json.dumps({"services": stack_services})))
    commands.on(["docker", "compose", "--project-name", "monitoring", "config", "--format", "json"], done(stdout=json.dumps({"services": monitoring_services or {}})))


def test_pinned_images_that_run_as_1000_pass(stack, commands, installed):
    merged(commands, {
        "sonarr": {"image": "lscr.io/linuxserver/sonarr:4" + PINNED, "environment": {"PUID": "1000", "PGID": "1000"},
                   "volumes": [{"type": "bind", "source": f"{installed.data}/volumes/sonarr"}]},
        "seerr": {"image": "ghcr.io/seerr-team/seerr:3" + PINNED, "user": "1000:1000",
                  "volumes": [{"type": "bind", "source": f"{installed.data}/volumes/seerr"}]},
    })
    stack.check()
    assert (installed.data / "volumes").is_dir()


def test_an_image_not_pinned_to_a_digest_is_named(stack, commands, capsys):
    merged(commands, {"sonarr": {"image": "lscr.io/linuxserver/sonarr:4"}})
    with pytest.raises(stack.commands.Stop) as stop:
        stack.check()
    assert str(stop.value) == "the merged compose files are not safe to deploy"
    assert capsys.readouterr().err == "sonarr: image lscr.io/linuxserver/sonarr:4 is not pinned to a digest\n"


def test_services_that_only_run_as_wiring_steps_are_checked_too(stack, commands):
    merged(commands, {"configarr": {"image": "ghcr.io/raydak-labs/configarr:1"}})
    with pytest.raises(stack.commands.Stop):
        stack.check()
    assert commands.env_of("--project-name", "media-server", "config")["COMPOSE_PROFILES"] == "wiring"


def test_an_unpinned_monitoring_image_is_named(stack, commands, capsys):
    merged(commands, {}, {"grafana": {"image": "grafana/grafana:13"}})
    with pytest.raises(stack.commands.Stop):
        stack.check()
    assert "grafana: image grafana/grafana:13 is not pinned to a digest" in capsys.readouterr().err


def test_a_service_writing_app_state_as_root_is_named(stack, commands, installed, capsys):
    merged(commands, {"extra": {"image": "example/extra:1" + PINNED, "volumes": [{"type": "bind", "source": f"{installed.data}/volumes/extra"}]}})
    with pytest.raises(stack.commands.Stop):
        stack.check()
    assert capsys.readouterr().err == "extra: writes app state but does not run as uid and gid 1000\n"


def test_a_service_binding_paths_outside_the_volumes_may_run_as_anyone(stack, commands, installed):
    merged(commands, {"jellyfin": {"image": "jellyfin" + PINNED, "volumes": [{"type": "bind", "source": f"{installed.data}/media"}, {"type": "volume", "source": "x"}]}})
    stack.check()


def test_compose_files_that_cannot_be_merged_stop_the_check(stack, commands):
    commands.on(["docker", "compose", "--project-name", "media-server", "config"], done(stderr="yaml: line 1: did not find expected key\n", returncode=15))
    with pytest.raises(stack.commands.Stop) as stop:
        stack.check()
    assert str(stop.value) == "the compose files cannot be merged; see the error above"


def docker_available():
    return shutil.which("docker") is not None and subprocess.run(["docker", "compose", "version"], capture_output=True).returncode == 0


@pytest.mark.skipif(not docker_available(), reason="needs docker compose")
def test_the_config_template_passes_the_checks(stack, commands, installed):
    for name in ("docker-compose.yml", "docker-compose.monitoring.yml"):
        shutil.copy(REPO / name, installed.engine / name)
    for image_file in (REPO / "config-template").glob("images*.yml"):
        shutil.copy(image_file, installed.config / image_file.name)
    secrets = installed.engine / ".secrets"
    secrets.mkdir()
    for name in ("vpn.env", "sonarr.env", "radarr.env", "prowlarr.env", "portainer_admin", "homepage.env", "gluetun.env", "healthchecks.env", "apps.env"):
        (secrets / name).touch()
    (installed.engine / ".env").write_text("DOCKER_GID=0\nHOMEPAGE_ALLOWED_HOSTS=media.local\n")
    commands.on(["docker"], real())
    stack.check()
