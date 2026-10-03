#!/usr/bin/env python3
import http.client
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from collections import namedtuple

ENGINE_DIR = os.environ["ENGINE_DIR"]
API_URL = os.environ.get("HEALTHCHECKS_API_URL", "https://healthchecks.io/api/v3/checks/")
LOCALTIME = os.environ.get("HEALTHCHECKS_LOCALTIME", "/etc/localtime")
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
        "channels": "*",
        "unique": ["slug"],
    }


def manage_key():
    with open(os.path.join(ENGINE_DIR, ".secrets", "healthchecks.env")) as env:
        lines = env.read().splitlines()
    return dict(line.partition("=")[::2] for line in lines if line).get("HEALTHCHECKS_MANAGE_KEY", "")


def set_up(key, body):
    request = urllib.request.Request(API_URL, data=json.dumps(body).encode(), method="POST")
    request.add_header("X-Api-Key", key)
    request.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(request, timeout=10):
        pass


def refusal(error):
    try:
        reason = json.loads(error.read()).get("error")
    except ValueError:
        reason = None
    return f"healthchecks.io answered {error.code}" + (f": {reason}" if reason else "")


def warn(slug, reason):
    print(f"healthchecks: could not set up {slug}: {reason}", file=sys.stderr)


def sync(checks, facts):
    key = manage_key()
    if not key:
        return
    set_up_slugs = []
    for job, slug in checks:
        try:
            set_up(key, payload(job, slug, facts))
            set_up_slugs.append(slug)
        except urllib.error.HTTPError as error:
            warn(slug, refusal(error))
        except (OSError, http.client.HTTPException) as error:
            warn(slug, getattr(error, "reason", error))
    if set_up_slugs:
        print(f"healthchecks: set up {', '.join(set_up_slugs)}")


def timers_time_zone():
    try:
        zone = subprocess.run(
            ["timedatectl", "show", "-p", "Timezone", "--value"], capture_output=True, text=True, check=True, timeout=10
        ).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        zone = ""
    return zone or linked_time_zone()


def linked_time_zone():
    try:
        link = os.readlink(LOCALTIME)
    except OSError:
        return "Etc/UTC"
    _, found, zone = link.partition("zoneinfo/")
    return zone if found else "Etc/UTC"


def facts_from_env():
    return Facts(
        name=os.environ["INSTALLATION_NAME"],
        tz=timers_time_zone(),
        repository=os.environ.get("RESTIC_REPOSITORY", ""),
        ssh=os.environ["HEALTHCHECKS_SSH"],
        directory=os.environ["HEALTHCHECKS_DIRECTORY"],
    )


if __name__ == "__main__":
    sync([argument.split("=", 1) for argument in sys.argv[1:]], facts_from_env())
