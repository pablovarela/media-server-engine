import socket

import pytest

from conftest import done, fresh_engine


@pytest.fixture
def ports(dirs, commands):
    commands.on(["docker", "port", "homepage", "3000"], done(stderr="Error: No such container: homepage\n", returncode=1))
    return fresh_engine("engine.ports")


@pytest.fixture
def listening():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        listener.listen()
        yield listener.getsockname()[1]


def free_port():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def test_a_port_something_listens_on_is_in_use(ports, listening):
    assert ports.port_in_use(listening) is True
    assert ports.main([str(listening)]) == 0


def test_a_port_nothing_listens_on_is_free(ports):
    port = free_port()
    assert ports.port_in_use(port) is False
    assert ports.main([str(port)]) == 1


def test_a_port_held_by_this_installations_own_landing_page_counts_as_free(ports, commands, listening):
    commands.on(["docker", "port", "homepage", "3000"], done(stdout=f"0.0.0.0:{listening}\n[::]:{listening}\n"))
    assert ports.port_in_use(listening) is False


def test_the_landing_page_on_another_port_does_not_free_this_one(ports, commands, listening):
    commands.on(["docker", "port", "homepage", "3000"], done(stdout=f"0.0.0.0:{listening + 1}\n"))
    assert ports.port_in_use(listening) is True



@pytest.mark.parametrize("argv", [[], ["80", "443"], ["http"]])
def test_the_port_check_needs_one_port_number(ports, capsys, argv):
    assert ports.main(argv) == 1
    assert capsys.readouterr().err == "port-in-use: usage: engine-run port-in-use PORT\n"
