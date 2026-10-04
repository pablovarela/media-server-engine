import http.client
import time
from unittest import mock

import pytest

from conftest import REPO, done, fresh_engine, http_error, refused

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


def machine(monkeypatch, commands, localtime, timedatectl):
    monkeypatch.setenv("TZ", "Europe/London")
    monkeypatch.setenv("HEALTHCHECKS_LOCALTIME", str(localtime))
    commands.on(["timedatectl", "show", "-p", "Timezone", "--value"], timedatectl)


def linked_to(tmp_path, target):
    localtime = tmp_path / "localtime"
    localtime.symlink_to(target)
    return localtime


def test_the_checks_use_the_time_zone_systemd_runs_the_timers_in(dirs, tmp_path, monkeypatch, commands):
    machine(monkeypatch, commands, linked_to(tmp_path, "/usr/share/zoneinfo/Europe/London"), done(stdout="America/New_York\n"))
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "America/New_York"


@pytest.mark.parametrize(
    "timedatectl",
    [done(returncode=127), done(stderr="Failed to connect to bus\n", returncode=1), done(stdout="\n")],
    ids=["no timedatectl", "timedated unreachable", "no answer"],
)
def test_without_an_answer_from_systemd_the_zone_comes_from_the_localtime_link(dirs, tmp_path, monkeypatch, commands, timedatectl):
    machine(monkeypatch, commands, linked_to(tmp_path, "../usr/share/zoneinfo/America/New_York"), timedatectl)
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "America/New_York"


def test_a_localtime_link_outside_zoneinfo_is_not_sent_as_a_zone(dirs, tmp_path, monkeypatch, commands):
    machine(monkeypatch, commands, linked_to(tmp_path, "/etc/writable/localtime"), done(returncode=127))
    assert fresh_engine("engine.healthchecks").timers_time_zone() == "Etc/UTC"


def test_a_machine_without_a_time_zone_runs_its_timers_in_utc(dirs, tmp_path, monkeypatch, commands):
    machine(monkeypatch, commands, tmp_path / "missing", done(returncode=127))
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


PINGS = "https://hc-ping.com/pk"


@pytest.fixture
def pinging(dirs, monkeypatch):
    monkeypatch.setenv("INSTALLATION_NAME", "testinst")
    monkeypatch.setenv("HEALTHCHECKS_PING_KEY", "pk")
    monkeypatch.setattr(time, "sleep", mock.Mock())
    return fresh_engine("engine.healthchecks")


def test_a_check_is_named_after_the_installation_and_a_secondarys_update_after_its_host(pinging, monkeypatch):
    monkeypatch.setattr(pinging.installation, "short_hostname", lambda: "laptop")
    assert pinging.slug("backup") == "testinst-backup"
    assert pinging.slug("update", "main") == "testinst-update"
    assert pinging.slug("update", "secondary") == "testinst-update-laptop"
    assert pinging.slug("backup", "secondary") == "testinst-backup"


def test_a_ping_creates_the_check_on_first_use_and_puts_the_suffix_before_the_query(pinging, http):
    http.on("GET", f"{PINGS}/testinst-backup/fail?create=1", {})
    pinging.ping("backup", "/fail")
    assert [r.path for r in http.requests] == ["/pk/testinst-backup/fail?create=1"]


def test_a_refused_ping_is_tried_again(pinging, http):
    http.on("GET", f"{PINGS}/testinst-verify?create=1", refused(), {})
    pinging.ping("verify")
    assert len(http.requests) == 2
    time.sleep.assert_called_once_with(1)


def test_a_ping_that_never_gets_through_warns_without_the_key_and_carries_on(pinging, urlopen, capsys):
    pinging.ping("backup", "/start")
    assert urlopen.call_count == 4
    assert [call.args[0] for call in time.sleep.call_args_list] == [1, 2, 4]
    err = capsys.readouterr().err
    assert err == "healthchecks: could not report testinst-backup/start: no answer was set up for this request\n"
    assert "pk" not in err


def test_a_ping_healthchecks_refuses_is_not_tried_again(pinging, http, capsys):
    http.on("GET", f"{PINGS}/testinst-backup?create=1", http_error(404))
    pinging.ping("backup")
    assert len(http.requests) == 1
    assert capsys.readouterr().err == "healthchecks: could not report testinst-backup: healthchecks.io answered 404\n"


def test_without_a_ping_key_nothing_is_sent_and_a_warning_is_logged(pinging, monkeypatch, urlopen, capsys):
    monkeypatch.delenv("HEALTHCHECKS_PING_KEY")
    pinging.ping("backup", "/fail")
    urlopen.assert_not_called()
    assert capsys.readouterr().err == "no healthchecks ping key configured; not reporting backup/fail\n"
