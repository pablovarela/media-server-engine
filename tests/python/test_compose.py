import os

import pytest

from conftest import done, fresh_engine


@pytest.fixture
def compose(dirs, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ.pop("COMPOSE_PROFILES", None)
    return fresh_engine("engine.compose")


def pin_homepage(dirs):
    (dirs.config / "images.yml").write_text("services:\n  homepage:\n    image: ghcr.io/gethomepage/homepage:v2.4.0@sha256:abc\n")


def test_the_stack_runs_from_the_engine_with_the_configs_images(compose, dirs, commands):
    commands.on(["docker", "compose"])
    compose.run("up", "-d", "--remove-orphans")
    assert commands.ran[0].args == [
        "docker", "compose", "--project-name", "media-server", "--project-directory", str(dirs.engine),
        "--env-file", f"{dirs.engine}/.env", "-f", f"{dirs.engine}/docker-compose.yml", "-f", f"{dirs.config}/images.yml",
        "up", "-d", "--remove-orphans",
    ]


def test_a_compose_override_in_the_config_is_added_last(compose, dirs, commands):
    (dirs.config / "compose.override.yml").write_text("services: {}\n")
    commands.on(["docker", "compose"])
    compose.run("up", "-d")
    assert commands.ran[0].args[12:14] == ["-f", f"{dirs.config}/compose.override.yml"]


def test_a_pinned_landing_page_is_a_profile_the_stack_runs_with(compose, dirs, commands):
    pin_homepage(dirs)
    commands.on(["docker", "compose"])
    compose.run("up", "-d")
    compose.run("pull", wiring=True)
    assert commands.ran[0].env["COMPOSE_PROFILES"] == "homepage"
    assert commands.ran[1].env["COMPOSE_PROFILES"] == "wiring,homepage"


def test_without_optional_services_no_profile_is_set(compose, dirs, commands):
    commands.on(["docker", "compose"])
    compose.run("up", "-d")
    assert commands.ran[0].env["COMPOSE_PROFILES"] == ""


def test_an_unreadable_images_file_pins_no_optional_service(compose, dirs):
    (dirs.config / "images.yml").write_text("services: [\n")
    assert compose.optional_services_pinned() == ""


@pytest.mark.parametrize("images", ["services:\n  - homepage\n", "services:\n  homepage: latest\n"])
def test_an_images_file_shaped_unexpectedly_pins_no_optional_service(compose, dirs, images):
    (dirs.config / "images.yml").write_text(images)
    assert compose.optional_services_pinned() == ""


def test_the_stack_command_runs_compose_with_its_arguments_and_ends_with_its_status(compose, dirs, commands):
    commands.on(["docker", "compose"], done(returncode=3))
    assert compose.stack_main(["ps"]) == 3
    assert commands.ran[0].args[:4] == ["docker", "compose", "--project-name", "media-server"]
    assert commands.ran[0].args[-1] == "ps"


def test_the_monitoring_command_runs_the_monitoring_project_with_its_images(compose, dirs, commands):
    commands.on(["docker", "compose"])
    assert compose.monitoring_main(["up", "-d"]) == 0
    assert commands.ran[0].args == [
        "docker", "compose", "--project-name", "monitoring", "--project-directory", str(dirs.engine), "--env-file", f"{dirs.engine}/.env",
        "-f", f"{dirs.engine}/docker-compose.monitoring.yml", "-f", f"{dirs.config}/images.monitoring.yml", "up", "-d",
    ]
