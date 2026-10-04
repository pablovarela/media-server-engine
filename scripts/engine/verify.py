import contextlib
import os
import shutil
import sqlite3
import tempfile

from engine import commands, healthchecks, installation, program, restic, role

DATABASE_SUFFIXES = (".db", ".sqlite", ".sqlite3")


def restored_databases(directory):
    return sorted(
        os.path.join(folder, name)
        for folder, _, files in os.walk(directory)
        for name in files
        if name.endswith(DATABASE_SUFFIXES) and os.path.isfile(os.path.join(folder, name))
    )


def is_sqlite_database(path):
    with open(path, "rb") as database:
        return database.read(15).replace(b"\x00", b"") == b"SQLite format 3"


def check_database(path, name):
    try:
        with contextlib.closing(sqlite3.connect(path)) as database:
            result = "\n".join(str(row[0]) for row in database.execute("PRAGMA integrity_check"))
    except sqlite3.Error as error:
        raise commands.Stop(f"cannot check {name}: {error}") from None
    if result != "ok":
        raise commands.Stop(f"integrity check failed for {name}: {result}")


def require_main():
    status = role.is_main()
    if status == role.NOT_THE_MAIN:
        raise commands.Stop(f"{role.explain(status)}; its verification runs there")
    if status:
        raise commands.Stop(f"{role.explain(status)}; nothing was checked")


def verify():
    directory = tempfile.mkdtemp(prefix="media-verify.", dir=os.environ.get("TMPDIR") or "/var/tmp")
    succeeded = False
    try:
        healthchecks.ping("verify", "/start")
        require_main()
        restic.run("unlock")
        restic.run_explaining_locks("check", "--retry-lock", "2h")
        restic.run_explaining_locks(
            "restore", "--retry-lock", "2h", "latest", *restic.snapshot_filter(), "--target", directory,
            "--include", "*.db", "--include", "*.sqlite", "--include", "*.sqlite3",
        )
        databases = restored_databases(directory)
        if not databases:
            raise commands.Stop("the latest snapshot holds no databases")
        for database in databases:
            if is_sqlite_database(database):
                check_database(database, os.path.relpath(database, directory))
        healthchecks.ping("verify")
        succeeded = True
    finally:
        with program.finishing():
            shutil.rmtree(directory, ignore_errors=True)
            if not succeeded:
                healthchecks.ping("verify", "/fail")


def run_verify(argv):
    installation.load_installation()
    verify()


def main(argv):
    return program.run("verify-backup", run_verify, argv)
