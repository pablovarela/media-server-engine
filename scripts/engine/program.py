import contextlib
import os
import signal
import sys

from engine import commands, installation


ENDING_SIGNALS = (signal.SIGTERM, signal.SIGHUP)


def stop_on_terminate(signum, frame):
    raise SystemExit(128 + signum)


@contextlib.contextmanager
def finishing():
    shielded = (*ENDING_SIGNALS, signal.SIGINT)
    previous = {signum: signal.signal(signum, signal.SIG_IGN) for signum in shielded}
    try:
        yield
    finally:
        for signum, handler in previous.items():
            signal.signal(signum, handler)


def start():
    sys.stdout.reconfigure(line_buffering=True)
    installation.export_directories()
    for name in ("HEALTHCHECKS_API_KEY", "HEALTHCHECKS_MANAGE_KEY"):
        os.environ.pop(name, None)
    for signum in ENDING_SIGNALS:
        signal.signal(signum, stop_on_terminate)


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
