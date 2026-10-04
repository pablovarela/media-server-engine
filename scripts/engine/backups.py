import fcntl
import os

from engine import commands, compose, healthchecks, installation, program, restic, role


def running_backup_pid():
    lock = os.path.join(installation.data_dir(), ".backup.lock")
    if not os.path.isfile(lock):
        return ""
    with open(lock) as held:
        try:
            fcntl.flock(held, fcntl.LOCK_SH | fcntl.LOCK_NB)
        except OSError:
            return held.read().strip() or "unknown"
    return ""


EXCLUDES = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "backup-excludes.txt")


def lock_path():
    return os.path.join(installation.data_dir(), ".backup.lock")


def take_lock():
    held = open(lock_path(), "a")
    try:
        fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        held.close()
        raise commands.Stop(f"a backup is already running (process {running_backup_pid()})") from None
    with open(lock_path(), "w") as pid:
        pid.write(f"{os.getpid()}\n")
    return held


def require_main():
    status = role.is_main()
    if status == 0:
        return
    if status == role.NOT_THE_MAIN:
        marker = os.path.join(installation.data_dir(), ".backup-main")
        if os.path.exists(marker):
            os.remove(marker)
        raise commands.Stop(f"{role.explain(status)}; this machine does not back up (make claim-backup-main makes it the main)")
    raise commands.Stop(f"{role.explain(status)}; nothing was backed up")


def start(services):
    if services:
        compose.run("start", *services)


def backup(claim=False):
    name = os.environ["INSTALLATION_NAME"]
    os.chdir(installation.data_dir())
    held = take_lock()
    stopped = []
    succeeded = False
    try:
        healthchecks.ping("backup", "/start")
        if not claim:
            require_main()
        machine = role.machine_id()
        stopped = compose.output("ps", "--status", "running", "--services").split()
        restic.run("unlock")
        compose.run("stop")
        restic.run_explaining_locks(
            "backup", "--retry-lock", "2h", "--host", name, "--tag", f"machine:{machine}",
            "--tag", f"machine-name:{installation.short_hostname()}", "--tag", "nightly", "--exclude-file", EXCLUDES, "volumes",
        )
        start(stopped)
        stopped = []
        restic.run_explaining_locks("forget", "--retry-lock", "2h", "--host", name, "--prune", "--keep-daily", "7", "--keep-weekly", "4", "--keep-monthly", "6")
        open(os.path.join(installation.data_dir(), ".backup-main"), "a").close()
        healthchecks.ping("backup")
        succeeded = True
    finally:
        try:
            start(stopped)
        except commands.CommandFailed:
            succeeded = False
        held.close()
        if not succeeded:
            healthchecks.ping("backup", "/fail")


def run_backup(argv):
    installation.load_installation()
    backup()


def main(argv):
    return program.run("backup", run_backup, argv)
