import json
import os
import sys

from engine import commands

NO_REPOSITORY = 10
LOCKED = 11


def run(*args):
    commands.run(["restic", *args])


def describe_locks():
    described = []
    for lock_id in commands.output(["restic", "list", "locks", "--no-lock"], check=False, discard_errors=True).split():
        try:
            lock = json.loads(commands.output(["restic", "cat", "lock", lock_id, "--no-lock"], check=False, discard_errors=True))
        except ValueError:
            continue
        described.append(
            "  {} lock from {} (process {}) since {}".format(
                "exclusive" if lock.get("exclusive") else "shared",
                lock.get("hostname", "an unknown host"),
                lock.get("pid", "?"),
                lock.get("time", "?"),
            )
        )
    return described


def run_explaining_locks(*args):
    returncode = commands.run(["restic", *args], check=False)
    if returncode == LOCKED:
        print("restic gave up waiting for a lock on the backup repository. Locks held:", file=sys.stderr)
        for line in describe_locks():
            print(line, file=sys.stderr)
        print("If none of those machines is running restic now, remove every lock with: make unlock-backup ALL=1", file=sys.stderr)
    if returncode:
        raise commands.CommandFailed(returncode)


def snapshot_filter():
    name = os.environ["INSTALLATION_NAME"]
    listing = commands.output(["restic", "snapshots", "--no-lock", "--host", name, "--json"], check=False, discard_errors=True)
    try:
        own = json.loads(listing) or []
    except ValueError:
        own = []
    return ["--host", name] if own else []
