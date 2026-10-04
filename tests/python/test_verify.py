import os
import sqlite3
import tempfile

import pytest

from conftest import done, fresh_engine

PINGS = "https://hc-ping.com/pk/testinst-verify"


def sqlite_file(path, damaged=False):
    db = sqlite3.connect(path)
    db.execute("create table t (x)")
    db.executemany("insert into t values (?)", [(str(n) * 500,) for n in range(200)])
    db.commit()
    db.close()
    if damaged:
        with open(path, "r+b") as file:
            file.seek(4096 * 2)
            file.write(b"\xff" * 4096)


def restored(target, damaged=False, files=None):
    app = os.path.join(target, "app")
    os.makedirs(app, exist_ok=True)
    for name in files if files is not None else ("a.db", "b.sqlite"):
        sqlite_file(os.path.join(app, name), damaged=damaged and name == "b.sqlite")
    with open(os.path.join(app, "portainer.db"), "wb") as bolt:
        bolt.write(b"\x00\x00\x10bolt")


def restore_answer(commands, **kwargs):
    def write():
        args = commands.ran[-1].args
        restored(args[args.index("--target") + 1], **kwargs)

    commands.on(["restic", "restore"], done(then=write))


@pytest.fixture
def verify(installed, commands, http, monkeypatch, tmp_path):
    os.environ["TMPDIR"] = str(tmp_path)
    commands.on(["restic"])
    commands.on(["restic", "snapshots"], done(stdout="[]"))
    restore_answer(commands)
    for suffix in ("/start", "/fail", ""):
        http.on("GET", f"{PINGS}{suffix}?create=1", {})
    module = fresh_engine("engine.verify")
    monkeypatch.setattr(module.role, "is_main", lambda: 0)
    return module


def pings(http):
    return [r.path.split("testinst-verify")[1] for r in http.requests]


def target(commands):
    args = next(command.args for command in commands.ran if command.args[:2] == ["restic", "restore"])
    return args[args.index("--target") + 1]


def test_verify_checks_the_repo_and_every_restored_database_then_pings_success(verify, commands, http):
    assert verify.main([]) == 0
    assert [command.args[:2] for command in commands.ran if command.args[0] == "restic"][:3] == [["restic", "unlock"], ["restic", "check"], ["restic", "snapshots"]]
    assert commands.did("restic", "check", "--retry-lock", "2h")
    assert commands.did("restic", "restore", "--retry-lock", "2h", "latest", "--target", "--include", "*.db", "--include", "*.sqlite", "--include", "*.sqlite3")
    assert pings(http) == ["/start?create=1", "?create=1"]


def test_verify_fails_and_names_a_damaged_database(verify, commands, http, capsys):
    restore_answer(commands, damaged=True)
    assert verify.main([]) == 1
    assert "app/b.sqlite" in capsys.readouterr().err
    assert pings(http)[-1] == "/fail?create=1"


def test_verify_fails_when_restic_check_fails(verify, commands, http):
    commands.on(["restic", "check"], done(returncode=1))
    assert verify.main([]) == 1
    assert pings(http)[-1] == "/fail?create=1"


def test_verify_fails_when_the_snapshot_holds_no_databases(verify, commands, http, capsys):
    commands.on(["restic", "restore"], done())
    assert verify.main([]) == 1
    assert capsys.readouterr().err == "verify-backup: the latest snapshot holds no databases\n"
    assert pings(http)[-1] == "/fail?create=1"


def test_verify_removes_its_temporary_restore_whether_it_worked_or_not(verify, commands):
    verify.main([])
    assert not os.path.exists(target(commands))
    commands.ran.clear()
    restore_answer(commands, damaged=True)
    verify.main([])
    assert not os.path.exists(target(commands))


def test_files_that_are_not_sqlite_are_skipped_without_a_warning(verify, commands, capsys):
    assert verify.main([]) == 0
    assert capsys.readouterr().err == ""


def test_verify_names_the_database_it_cannot_open(verify, commands, capsys):
    def unreadable():
        args = commands.ran[-1].args
        app = os.path.join(args[args.index("--target") + 1], "app")
        os.makedirs(app)
        with open(os.path.join(app, "a.db"), "wb") as broken:
            broken.write(b"SQLite format 3\x00" + b"\x00" * 84)

    commands.on(["restic", "restore"], done(then=unreadable))
    assert verify.main([]) == 1
    assert capsys.readouterr().err.startswith("verify-backup: cannot check app/a.db: ")


def test_verify_restores_to_disk_backed_var_tmp_unless_tmpdir_says_otherwise(verify, commands, monkeypatch):
    del os.environ["TMPDIR"]
    made = []
    monkeypatch.setattr(tempfile, "mkdtemp", lambda prefix, dir: made.append((prefix, dir)) or str(os.path.join(dir, prefix + "x")))
    commands.on(["restic", "restore"], done())
    verify.main([])
    assert made == [("media-verify.", "/var/tmp")]


def test_a_secondary_does_not_verify_the_mains_backups(verify, commands, monkeypatch, capsys):
    monkeypatch.setattr(verify.role, "is_main", lambda: verify.role.NOT_THE_MAIN)
    assert verify.main([]) == 1
    assert not commands.did("restic", "check")
    assert capsys.readouterr().err == "verify-backup: another machine is testinst's main; its verification runs there\n"


def test_a_backup_repository_that_cannot_be_read_is_reported_as_such(verify, monkeypatch, capsys):
    monkeypatch.setattr(verify.role, "is_main", lambda: verify.role.UNREADABLE)
    assert verify.main([]) == 1
    assert capsys.readouterr().err == "verify-backup: cannot read the backup repository to tell which machine is testinst's main; nothing was checked\n"


def test_verify_that_gives_up_waiting_for_a_restic_lock_says_who_holds_it(verify, commands, capsys):
    commands.on(["restic", "list", "locks"], done(stdout="5f3a9c\n"))
    commands.on(["restic", "cat", "lock"], done(stdout='{"time":"2026-09-30T04:37:23+01:00","exclusive":false,"hostname":"laptop","pid":6116}'))
    commands.on(["restic", "check"], done(returncode=11))
    assert verify.main([]) == 11
    err = capsys.readouterr().err
    assert "laptop" in err and "make unlock-backup" in err


def test_the_restore_uses_the_installations_own_snapshots_when_it_has_any(verify, commands):
    commands.on(["restic", "snapshots"], done(stdout='[{"id": "x"}]'))
    verify.main([])
    assert commands.did("restic", "restore", "latest", "--host", "testinst", "--target")


def test_a_database_whose_integrity_check_finds_problems_fails_the_verify_with_them(verify, commands, monkeypatch, capsys):
    def header_only():
        args = commands.ran[-1].args
        app = os.path.join(args[args.index("--target") + 1], "app")
        os.makedirs(app)
        with open(os.path.join(app, "a.db"), "wb") as database:
            database.write(b"SQLite format 3\x00")

    commands.on(["restic", "restore"], done(then=header_only))

    class Checked:
        def execute(self, statement):
            return [("*** in database main ***",), ("row 1 missing from index i",)]

        def close(self):
            pass

    monkeypatch.setattr(sqlite3, "connect", lambda path: Checked())
    assert verify.main([]) == 1
    assert capsys.readouterr().err == "verify-backup: integrity check failed for app/a.db: *** in database main ***\nrow 1 missing from index i\n"
