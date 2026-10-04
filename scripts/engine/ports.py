import socket

from engine import commands, program


def something_listens(port):
    with socket.socket() as probe:
        probe.settimeout(1)
        return probe.connect_ex(("127.0.0.1", port)) == 0


def this_landing_page_listens(port):
    published = commands.output(["docker", "port", "homepage", "3000"], check=False, discard_errors=True)
    return any(line.endswith(f":{port}") for line in published.splitlines())


def port_in_use(port):
    return something_listens(port) and not this_landing_page_listens(port)


def check(argv):
    if len(argv) != 1 or not argv[0].isdigit():
        raise commands.Stop("usage: engine-run port-in-use PORT")
    return 0 if port_in_use(int(argv[0])) else 1


def main(argv):
    return program.run("port-in-use", check, argv)
