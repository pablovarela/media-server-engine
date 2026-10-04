import os
import signal
import sys

from engine import commands, installation


def stop_on_terminate(signum, frame):
    raise SystemExit(128 + signum)


def start():
    sys.stdout.reconfigure(line_buffering=True)
    installation.export_directories()
    for name in ("HEALTHCHECKS_API_KEY", "HEALTHCHECKS_MANAGE_KEY"):
        os.environ.pop(name, None)
    signal.signal(signal.SIGTERM, stop_on_terminate)


def run(name, function, argv):
    try:
        return function(argv) or 0
    except commands.Stop as stop:
        print(stop.text(name), file=sys.stderr)
        return 1
    except commands.CommandFailed as failed:
        return failed.returncode
    except (OSError, ValueError) as error:
        print(f"{name}: {error}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130
