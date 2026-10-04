import pytest

from conftest import done, fresh_engine


@pytest.fixture
def version(installed, commands):
    (installed.config / "engine.env").write_text("ENGINE_VERSION=v1.0.0\n")
    commands.on(["git", "-C", str(installed.engine), "describe", "--tags", "--always"], done(stdout="v1.0.0\n"))
    return fresh_engine("engine.version")


def test_the_running_engine_and_the_pinned_one_are_shown(version, capsys):
    assert version.main([]) == 0
    assert capsys.readouterr().out == "engine: v1.0.0\nconfig pins: v1.0.0\n"


def test_a_difference_says_what_make_update_will_do(version, commands, installed, capsys):
    commands.on(["git", "-C", str(installed.engine), "describe"], done(stdout="v0.9.0-2-gabc1234\n"))
    version.main([])
    assert capsys.readouterr().out == "engine: v0.9.0-2-gabc1234\nconfig pins: v1.0.0\nThey differ: make update switches the engine to v1.0.0.\n"


def test_a_local_pin_never_differs(version, commands, installed, capsys):
    (installed.config / "engine.env").write_text("ENGINE_VERSION=local\n")
    commands.on(["git", "-C", str(installed.engine), "describe"], done(stdout="v0.9.0\n"))
    version.main([])
    assert capsys.readouterr().out == "engine: v0.9.0\nconfig pins: local\n"


def test_an_engine_git_cannot_describe_and_a_config_without_a_pin(version, commands, installed, capsys):
    (installed.config / "engine.env").unlink()
    commands.on(["git", "-C", str(installed.engine), "describe"], done(returncode=128))
    version.main([])
    assert capsys.readouterr().out == "engine: unknown\nconfig pins: nothing\n"
