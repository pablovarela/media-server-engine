#!/usr/bin/env python3
import os
from collections import namedtuple

ENGINE_DIR = os.environ["ENGINE_DIR"]
API_URL = os.environ.get("HEALTHCHECKS_API_URL", "https://healthchecks.io/api/v3/checks/")
HOUR = 3600
WEEKDAYS = ["Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"]

Check = namedtuple("Check", "schedule grace unit target")
Facts = namedtuple("Facts", "name tz repository ssh directory")

CHECKS = {
    "backup": Check("30 4 * * *", 2 * HOUR, "media-backup", "make backup-now"),
    "update": Check("0 5 * * *", 2 * HOUR, "media-update", "make update"),
    "verify": Check("30 5 * * 0", 4 * HOUR, "media-verify", "make verify-backup-now"),
}


def when(schedule):
    minute, hour, _day, _month, weekday = schedule.split()
    time = f"{int(hour):02d}:{int(minute):02d}"
    return f"daily at {time}" if weekday == "*" else f"on {WEEKDAYS[int(weekday)]} at {time}"


def backup_destination(repository):
    if repository.startswith("b2:"):
        return "Backblaze B2"
    if repository.startswith("/"):
        return "a local folder"
    return f"{repository.split(':', 1)[0]} storage"


def description(job, facts):
    check = CHECKS[job]
    runs = f"Runs {when(check.schedule)}"
    timer = f"via the {check.unit} timer ({check.target})"
    login = f"If it fails: ssh {facts.ssh}, cd {facts.directory}, journalctl -u {check.unit}.service"
    if job == "backup":
        return (
            f"Nightly backup of {facts.name}'s app state (libraries, history, users, settings) "
            f"to {backup_destination(facts.repository)} with restic. Media files are not included. "
            f"{runs} on the main machine {timer}. {login} --since today; make unlock-backup clears a stale lock."
        )
    if job == "verify":
        return (
            f"Weekly check that {facts.name}'s backups in {backup_destination(facts.repository)} can be read back (restic check). "
            f"{runs} on the main machine {timer}. {login} --since -7d -n 200."
        )
    return (
        f"Daily update of {facts.name}: pulls the config and the pinned engine version, pulls images, brings the apps up and wires them. "
        f"{runs} {timer}. It refuses to run while the config or engine has uncommitted changes. "
        f"{login} --since today, then sudo systemctl start {check.unit}.service to retry."
    )


def payload(job, slug, facts):
    check = CHECKS[job]
    return {
        "name": slug,
        "slug": slug,
        "tags": job,
        "desc": description(job, facts),
        "schedule": check.schedule,
        "tz": facts.tz,
        "grace": check.grace,
        "unique": ["slug"],
    }
