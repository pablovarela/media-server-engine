import http.client
import subprocess
from unittest import mock

import pytest

from conftest import REPO, fresh_engine, http_error

API = "https://healthchecks.io/api/v3/checks/"
MAIN = [("backup", "home-backup"), ("verify", "home-verify"), ("update", "home-update")]
WEEKDAY_NUMBERS = {"Sun": "0", "Mon": "1", "Tue": "2", "Wed": "3", "Thu": "4", "Fri": "5", "Sat": "6"}


@pytest.fixture
def healthchecks(dirs, monkeypatch):
    monkeypatch.delenv("HEALTHCHECKS_API_URL", raising=False)
    (dirs.engine / ".secrets").mkdir()
    return fresh_engine("engine.healthchecks")


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
    assert output.err == "healthchecks: could not set up home-backup: healthchecks.io answered 401: wrong api key\n"
    assert output.out == "healthchecks: set up home-verify, home-update\n"


def test_an_unreachable_healthchecks_is_reported_without_the_key(healthchecks, dirs, urlopen, capsys):
    with_manage_key(dirs)
    healthchecks.sync([("update", "home-update")], facts(healthchecks))
    output = capsys.readouterr()
    assert output.err == "healthchecks: could not set up home-update: no answer was set up for this request\n"
    assert "hc-manage" not in output.err + output.out


def machine(monkeypatch, localtime, timedatectl):
    monkeypatch.setenv("TZ", "Europe/London")
    monkeypatch.setenv("HEALTHCHECKS_LOCALTIME", str(localtime))
    run = mock.Mock(side_effect=timedatectl)
    monkeypatch.setattr(subprocess, "run", run)
    return run


def answers(zone):
    return lambda *args, **kwargs: subprocess.CompletedProcess(args[0], 0, stdout=f"{zone}\n", stderr="")


def no_timedatectl(*args, **kwargs):
    raise FileNotFoundError("timedatectl")


def timedated_unreachable(*args, **kwargs):
    raise subprocess.CalledProcessError(1, args[0], stderr="Failed to connect to bus")


def linked_to(tmp_path, target):
    localtime = tmp_path / "localtime"
    localtime.symlink_to(target)
    return localtime


def test_the_checks_use_the_time_zone_systemd_runs_the_timers_in(dirs, tmp_path, monkeypatch):
    run = machine(monkeypatch, linked_to(tmp_path, "/usr/share/zoneinfo/Europe/London"), answers("America/New_York"))
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "America/New_York"
    assert run.call_args.args[0] == ["timedatectl", "show", "-p", "Timezone", "--value"]


@pytest.mark.parametrize("timedatectl", [no_timedatectl, timedated_unreachable, answers("")])
def test_without_an_answer_from_systemd_the_zone_comes_from_the_localtime_link(dirs, tmp_path, monkeypatch, timedatectl):
    machine(monkeypatch, linked_to(tmp_path, "../usr/share/zoneinfo/America/New_York"), timedatectl)
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "America/New_York"


def test_a_localtime_link_outside_zoneinfo_is_not_sent_as_a_zone(dirs, tmp_path, monkeypatch):
    machine(monkeypatch, linked_to(tmp_path, "/etc/writable/localtime"), no_timedatectl)
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "Etc/UTC"


def test_a_machine_without_a_time_zone_runs_its_timers_in_utc(dirs, tmp_path, monkeypatch):
    machine(monkeypatch, tmp_path / "missing", no_timedatectl)
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "Etc/UTC"


def test_an_error_page_that_is_not_healthchecks_json_is_reported_by_its_status(healthchecks, dirs, http, capsys):
    with_manage_key(dirs)
    http.on("POST", API, http_error(502, "<html>bad gateway</html>"))
    healthchecks.sync([("update", "home-update")], facts(healthchecks))
    assert capsys.readouterr().err == "healthchecks: could not set up home-update: healthchecks.io answered 502\n"


def test_a_garbled_answer_is_reported_like_any_other_failure(healthchecks, dirs, urlopen, capsys):
    with_manage_key(dirs)
    urlopen.side_effect = http.client.BadStatusLine("garbage")
    healthchecks.sync([("update", "home-update"), ("backup", "home-backup")], facts(healthchecks))
    assert capsys.readouterr().err.splitlines() == [
        "healthchecks: could not set up home-update: garbage",
        "healthchecks: could not set up home-backup: garbage",
    ]


def test_a_hanging_healthchecks_holds_the_update_no_longer_than_a_ping_would(healthchecks, dirs, urlopen):
    with_manage_key(dirs)
    healthchecks.sync([("update", "home-update")], facts(healthchecks))
    assert urlopen.call_args.kwargs["timeout"] == 10
