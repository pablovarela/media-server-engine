import fcntl
import os

from engine import installation


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
