import json
import os
import sys
from secrets import token_hex

from engine import commands, installation, program, restic

NOT_THE_MAIN = 1
UNREADABLE = 2


class RepositoryUnreadable(commands.CommandFailed):
    pass


def has_content(path):
    return os.path.isfile(path) and os.path.getsize(path) > 0


def machine_id():
    system = os.environ.get("MACHINE_ID_FILE", "/etc/machine-id")
    if has_content(system):
        with open(system) as identity:
            return identity.read().strip()
    os.makedirs(installation.data_dir(), exist_ok=True)
    generated = os.path.join(installation.data_dir(), ".machine-id")
    if not has_content(generated):
        with open(generated, "w") as identity:
            identity.write(token_hex(16) + "\n")
    with open(generated) as identity:
        return identity.read().strip()


def latest_snapshot():
    returncode, listing, errors = commands.captured(
        ["restic", "snapshots", "--no-lock", "--host", os.environ["INSTALLATION_NAME"], "--latest", "1", "--json"]
    )
    if returncode == restic.NO_REPOSITORY:
        return None
    sys.stderr.write(errors)
    if returncode:
        raise RepositoryUnreadable(returncode)
    try:
        snapshots = sorted(json.loads(listing) or [], key=lambda snapshot: snapshot["time"])
    except (ValueError, KeyError, TypeError):
        raise RepositoryUnreadable(1) from None
    return snapshots[-1] if snapshots else None


def tags(snapshot):
    return dict(tag.split(":", 1) for tag in snapshot.get("tags", []) if ":" in tag)


def main_machine():
    snapshot = latest_snapshot()
    return tags(snapshot).get("machine", "") if snapshot else ""


def describe_main():
    snapshot = latest_snapshot()
    if not snapshot:
        return ""
    machine = tags(snapshot)
    name = machine.get("machine-name") or "machine " + machine.get("machine", "")
    return f"{name}, last backup {snapshot['time'][:16].replace('T', ' ')}"


def is_main():
    try:
        main = main_machine()
    except RepositoryUnreadable:
        return UNREADABLE
    return 0 if not main or main == machine_id() else NOT_THE_MAIN


def explain(status):
    name = os.environ["INSTALLATION_NAME"]
    if status == NOT_THE_MAIN:
        return f"another machine is {name}'s main"
    return f"cannot read the backup repository to tell which machine is {name}'s main"


def answer(argv):
    installation.load_installation()
    command = argv[0] if argv else ""
    if command == "is-main":
        return is_main()
    printers = {"machine-id": machine_id, "main-machine": main_machine, "describe-main": describe_main}
    if command not in printers:
        raise commands.Stop("usage: backup-role is-main|main-machine|describe-main|machine-id")
    printed = printers[command]()
    if printed:
        print(printed)


def main(argv):
    return program.run("backup-role", answer, argv)
