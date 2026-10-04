import pytest

from conftest import done, fresh_engine

LISTING = ["docker", "image", "ls", "-a", "--digests", "--format", "{{.Repository}} {{.Tag}} {{.Digest}}"]


@pytest.fixture
def images(installed, commands):
    (installed.config / "images.yml").write_text("services:\n  sonarr:\n    image: lscr.io/linuxserver/sonarr:4.0.21-ls327@sha256:newsonarr\n")
    (installed.config / "images.monitoring.yml").write_text("services:\n  grafana:\n    image: grafana/grafana:13.2.3@sha256:newgrafana\n")
    commands.on(LISTING, done(stdout="\n".join([
        "grafana/grafana latest sha256:oldgrafana",
        "postgres 16 sha256:unrelated",
        "lscr.io/linuxserver/sonarr <none> sha256:newsonarr",
        "lscr.io/linuxserver/sonarr <none> sha256:oldsonarr",
        "grafana/grafana <none> sha256:newgrafana",
    ]) + "\n"))
    commands.on(["docker", "image", "rm"])
    return fresh_engine("engine.images")


def removed(commands):
    return [command.args[3] for command in commands.ran if command.args[:3] == ["docker", "image", "rm"]]


def test_prune_removes_outdated_media_and_monitoring_images_and_says_so(images, commands, capsys):
    assert images.main([]) == 0
    assert removed(commands) == ["grafana/grafana:latest", "lscr.io/linuxserver/sonarr@sha256:oldsonarr"]
    assert capsys.readouterr().out == "removed grafana/grafana:latest\nremoved lscr.io/linuxserver/sonarr@sha256:oldsonarr\n"


def test_prune_keeps_every_pinned_image_and_leaves_other_projects_alone(images, commands):
    images.main([])
    assert not any("new" in image or "postgres" in image for image in removed(commands))


def test_prune_carries_on_when_an_image_is_still_in_use(images, commands, capsys):
    commands.on(["docker", "image", "rm", "grafana/grafana:latest"], done(stderr="conflict: image is being used\n", returncode=1))
    assert images.main([]) == 0
    assert capsys.readouterr().out == "kept grafana/grafana:latest (still in use)\nremoved lscr.io/linuxserver/sonarr@sha256:oldsonarr\n"


def test_prune_keeps_images_pinned_only_in_the_override(images, commands, installed):
    (installed.config / "compose.override.yml").write_text("services:\n  extra:\n    image: example/extra:1@sha256:extra1\n")
    commands.on(LISTING, done(stdout="example/extra <none> sha256:extra1\nexample/extra <none> sha256:extra0\n"))
    images.main([])
    assert removed(commands) == ["example/extra@sha256:extra0"]


def test_a_registry_with_a_port_keeps_its_port_in_the_repository(images, installed):
    (installed.config / "images.yml").write_text("services:\n  app:\n    image: registry.lan:5000/app:2@sha256:abc\n")
    assert "registry.lan:5000/app@sha256:abc" in images.pinned_references()


def test_files_that_are_missing_or_unreadable_pin_nothing(images, installed):
    (installed.config / "images.monitoring.yml").write_text("services: [\n")
    assert images.pinned_references() == {"lscr.io/linuxserver/sonarr@sha256:newsonarr"}
