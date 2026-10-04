from engine import installation, program


def show(argv):
    installation.load_installation()
    running = installation.engine_version() or "unknown"
    try:
        pinned = installation.pinned_engine_version()
    except FileNotFoundError:
        pinned = ""
    print(f"engine: {running}")
    print(f"config pins: {pinned or 'nothing'}")
    if pinned and pinned != "local" and pinned != running:
        print(f"They differ: make update switches the engine to {pinned}.")


def main(argv):
    return program.run("version", show, argv)
