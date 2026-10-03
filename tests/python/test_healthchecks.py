import pytest

from conftest import REPO, fresh_import, http_error

API = "https://healthchecks.io/api/v3/checks/"
MAIN = [("backup", "home-backup"), ("verify", "home-verify"), ("update", "home-update")]
WEEKDAY_NUMBERS = {"Sun": "0", "Mon": "1", "Tue": "2", "Wed": "3", "Thu": "4", "Fri": "5", "Sat": "6"}


@pytest.fixture
def healthchecks(dirs, monkeypatch):
    monkeypatch.delenv("HEALTHCHECKS_API_URL", raising=False)
    (dirs.engine / ".secrets").mkdir()
    return fresh_import("healthchecks")


def facts(healthchecks, repository="b2:bucket:home", tz="Europe/London"):
    return healthchecks.Facts(name="home", tz=tz, repository=repository, ssh="me@media.example", directory="~/home")


def cron_of(on_calendar):
    *weekday, _date, time = on_calendar.split()
    hour, minute, _second = time.split(":")
    return f"{int(minute)} {int(hour)} * * {WEEKDAY_NUMBERS[weekday[0]] if weekday else '*'}"


def on_calendar(job):
    timer = (REPO / "systemd" / f"media-{job}.timer").read_text()
    return next(line.split("=", 1)[1] for line in timer.splitlines() if line.startswith("OnCalendar="))


def test_each_check_runs_on_its_timers_schedule(healthchecks):
    for job, check in healthchecks.CHECKS.items():
        assert check.schedule == cron_of(on_calendar(job)), job


def test_the_backup_check_says_what_is_backed_up_where_and_what_to_do_when_it_fails(healthchecks):
    assert healthchecks.description("backup", facts(healthchecks)) == (
        "Nightly backup of home's app state (libraries, history, users, settings) to Backblaze B2 with restic. "
        "Media files are not included. Runs daily at 04:30 on the main machine via the media-backup timer (make backup-now). "
        "If it fails: ssh me@media.example, cd ~/home, journalctl -u media-backup.service --since today; "
        "make unlock-backup clears a stale lock."
    )


def test_the_verify_check_runs_weekly_and_looks_back_a_week_in_the_journal(healthchecks):
    assert healthchecks.description("verify", facts(healthchecks)) == (
        "Weekly check that home's backups in Backblaze B2 can be read back (restic check). "
        "Runs on Sundays at 05:30 on the main machine via the media-verify timer (make verify-backup-now). "
        "If it fails: ssh me@media.example, cd ~/home, journalctl -u media-verify.service --since -7d -n 200."
    )


def test_the_update_check_says_how_to_retry(healthchecks):
    assert healthchecks.description("update", facts(healthchecks)) == (
        "Daily update of home: pulls the config and the pinned engine version, pulls images, brings the apps up and wires them. "
        "Runs daily at 05:00 via the media-update timer (make update). "
        "It refuses to run while the config or engine has uncommitted changes. "
        "If it fails: ssh me@media.example, cd ~/home, journalctl -u media-update.service --since today, "
        "then sudo systemctl start media-update.service to retry."
    )


@pytest.mark.parametrize(
    "repository, destination",
    [("b2:bucket:home", "Backblaze B2"), ("b2:bucket", "Backblaze B2"), ("/mnt/backup/home", "a local folder"), ("sftp:backup@nas:/srv/restic", "sftp storage")],
)
def test_the_backup_destination_is_named_from_the_restic_repository(healthchecks, repository, destination):
    assert f" to {destination} with restic." in healthchecks.description("backup", facts(healthchecks, repository=repository))


def test_a_check_is_matched_by_its_slug_and_gets_its_schedule_grace_time_zone_and_tag(healthchecks):
    body = healthchecks.payload("verify", "home-verify", facts(healthchecks))
    assert body == {
        "name": "home-verify",
        "slug": "home-verify",
        "tags": "verify",
        "desc": healthchecks.description("verify", facts(healthchecks)),
        "schedule": "30 5 * * 0",
        "tz": "Europe/London",
        "grace": 14400,
        "channels": "*",
        "unique": ["slug"],
    }


def with_manage_key(dirs):
    (dirs.engine / ".secrets" / "healthchecks.env").write_text("HEALTHCHECKS_PING_KEY=ping\nHEALTHCHECKS_MANAGE_KEY=hc-manage\n")


def test_the_main_sets_up_its_three_checks_with_the_manage_key(healthchecks, dirs, http, capsys):
    with_manage_key(dirs)
    http.on("POST", API, {})
    healthchecks.sync(MAIN, facts(healthchecks))
    assert [r.body for r in http.requests] == [healthchecks.payload(job, slug, facts(healthchecks)) for job, slug in MAIN]
    assert {r.headers["X-api-key"] for r in http.requests} == {"hc-manage"}
    assert capsys.readouterr().out == "healthchecks: set up home-backup, home-verify, home-update\n"


def test_without_a_manage_key_nothing_is_sent_or_said(healthchecks, dirs, http, capsys):
    (dirs.engine / ".secrets" / "healthchecks.env").write_text("HEALTHCHECKS_PING_KEY=ping\n")
    healthchecks.sync(MAIN, facts(healthchecks))
    assert http.requests == []
    assert capsys.readouterr() == ("", "")


def test_a_check_healthchecks_refuses_is_reported_and_the_others_are_still_set_up(healthchecks, dirs, http, capsys):
    with_manage_key(dirs)
    http.on("POST", API, http_error(401, '{"error": "wrong api key"}'), {}, {})
    healthchecks.sync(MAIN, facts(healthchecks))
    output = capsys.readouterr()
    assert output.err == "healthchecks: could not set up home-backup: healthchecks.io answered 401\n"
    assert output.out == "healthchecks: set up home-verify, home-update\n"


def test_an_unreachable_healthchecks_is_reported_without_the_key(healthchecks, dirs, urlopen, capsys):
    with_manage_key(dirs)
    healthchecks.sync([("update", "home-update")], facts(healthchecks))
    output = capsys.readouterr()
    assert output.err == "healthchecks: could not set up home-update: no answer was set up for this request\n"
    assert "hc-manage" not in output.err + output.out


def installation(monkeypatch, localtime):
    monkeypatch.setenv("INSTALLATION_NAME", "home")
    monkeypatch.setenv("TZ", "Europe/London")
    monkeypatch.setenv("RESTIC_REPOSITORY", "/mnt/backup/home")
    monkeypatch.setenv("HEALTHCHECKS_SSH", "me@media.example")
    monkeypatch.setenv("HEALTHCHECKS_DIRECTORY", "~/home")
    monkeypatch.setenv("HEALTHCHECKS_LOCALTIME", str(localtime))


def test_the_checks_use_the_time_zone_the_machines_timers_run_in(dirs, tmp_path, monkeypatch):
    localtime = tmp_path / "localtime"
    localtime.symlink_to("/usr/share/zoneinfo/America/New_York")
    installation(monkeypatch, localtime)
    healthchecks = fresh_import("healthchecks")
    assert healthchecks.facts_from_env() == healthchecks.Facts("home", "America/New_York", "/mnt/backup/home", "me@media.example", "~/home")


def test_a_machine_without_a_time_zone_runs_its_timers_in_utc(dirs, tmp_path, monkeypatch):
    installation(monkeypatch, tmp_path / "missing")
    assert fresh_import("healthchecks").facts_from_env().tz == "Etc/UTC"
