import collections
import os
import pwd
import re
import shutil
import tempfile
import zoneinfo

from engine import commands, installation, ports, subtitle_languages

Field = collections.namedtuple("Field", "section name kind label")

FIELDS = [
    Field(*row.split("|"))
    for row in """Installation|TZ|timezone|Time zone
Installation|CONFIG_LOCATION|choice|Where to keep this config
Installation|GITHUB_OWNER|text|GitHub owner of the config repo
Installation|JELLYFIN_ADMIN_USER|text|Jellyfin admin user
Backup|BACKUP_TYPE|choice|Where to keep the backups
Backup|BACKUP_FOLDER|text|Backup folder
Backup|BACKUP_URL|text|Restic repository
Backup|B2_BUCKET|text|B2 bucket
Backup|B2_FOLDER|text|Folder inside the bucket
Backup|B2_ACCOUNT_ID|text|B2 key ID
Backup|B2_ACCOUNT_KEY|secret|B2 application key
Backup|RESTIC_PASSWORD|password|Restic password
VPN|VPN_SERVICE_PROVIDER|text|VPN provider (gluetun name)
VPN|OPENVPN_USER|secret|OpenVPN user
VPN|OPENVPN_PASSWORD|secret|OpenVPN password
VPN|SERVER_COUNTRIES|text|VPN server countries
Healthchecks (optional)|HEALTHCHECKS_PING_KEY|secret|healthchecks.io project ping key
Healthchecks (optional)|HEALTHCHECKS_API_KEY|secret|healthchecks.io read-only API key (backup status on the landing page)
Healthchecks (optional)|HEALTHCHECKS_MANAGE_KEY|secret|healthchecks.io read-write API key (sets up the checks' schedules)
App logins|JELLYFIN_ADMIN_PASSWORD|password|Jellyfin admin password
App logins|DELUGE_WEB_PASSWORD|password|Deluge web password
App logins|PORTAINER_ADMIN_PASSWORD|password|Portainer admin password (at least 12 characters)
Subtitles|SUBTITLE_LANGUAGES|text|Subtitle languages (codes, comma separated)
Landing page|HOMEPAGE_PORT|text|Landing page port""".splitlines()
]

CHOICES = {
    "CONFIG_LOCATION": [("local", "Only on this machine"), ("github", "A private GitHub repo, so other machines can join")],
    "BACKUP_TYPE": [
        ("local", "A folder on this machine or on a mounted disk"),
        ("b2", "Backblaze B2"),
        ("other", "Another restic repository (sftp:, s3:, rest: ...), its credentials added to secrets/backup.sops.env by hand"),
    ],
}

ASKED_ONLY_WHEN = {
    "GITHUB_OWNER": ("CONFIG_LOCATION", "github"),
    "BACKUP_FOLDER": ("BACKUP_TYPE", "local"),
    "BACKUP_URL": ("BACKUP_TYPE", "other"),
    "B2_BUCKET": ("BACKUP_TYPE", "b2"),
    "B2_FOLDER": ("BACKUP_TYPE", "b2"),
    "B2_ACCOUNT_ID": ("BACKUP_TYPE", "b2"),
    "B2_ACCOUNT_KEY": ("BACKUP_TYPE", "b2"),
}

HELP = {
    "CONFIG_LOCATION": "You can switch at any time with make configure: switching to GitHub publishes the config, switching back to local keeps the GitHub repo.",
    "BACKUP_FOLDER": "An absolute path; ~ stands for your home folder. It is created if it does not exist.",
    "BACKUP_URL": "As restic takes it in -r, such as sftp:user@host:/srv/restic or s3:s3.amazonaws.com/bucket/restic. It is not checked here.",
    "TZ": "A name from the time zone database, such as Europe/London or America/New_York.",
}

PORTAINER_PASSWORD_MINIMUM = 12
RESTIC_REPOSITORY_DOES_NOT_EXIST = 10
RESTIC_WRONG_PASSWORD = 12
LANDING_PAGE_PORTS = [80, *range(8080, 8100)]


def backup_check_seconds():
    return float(os.environ.get("BACKUP_CHECK_SECONDS", "45"))


def first_free_landing_page_port():
    return str(next((port for port in LANDING_PAGE_PORTS if not ports.port_in_use(port)), 80))


def engine_owner():
    found = re.search(r"github\.com[:/]([^/]+)/", installation.engine_git("remote", "get-url", "origin"))
    return found.group(1) if found else ""


def current_subtitle_languages():
    try:
        with open("apps.yml") as apps:
            languages = subtitle_languages.languages_in(apps.read())
    except FileNotFoundError:
        languages = None
    return ", ".join(languages or ["en"])


def default(name, values):
    if name == "SUBTITLE_LANGUAGES":
        return current_subtitle_languages()
    if name == "B2_FOLDER":
        return values.get(name, "restic")
    installation_name = values["INSTALLATION_NAME"]
    fallbacks = {
        "TZ": lambda: "Etc/UTC",
        "CONFIG_LOCATION": lambda: "local",
        "GITHUB_OWNER": engine_owner,
        "JELLYFIN_ADMIN_USER": lambda: "admin",
        "BACKUP_TYPE": lambda: "local",
        "BACKUP_FOLDER": lambda: os.path.join(os.path.expanduser("~"), f"{installation_name}-backups"),
        "B2_BUCKET": lambda: f"{installation_name}-media-server-backup",
        "HOMEPAGE_PORT": first_free_landing_page_port,
    }
    return values.get(name) or fallbacks.get(name, lambda: "")()


def applies(name, values):
    if name not in ASKED_ONLY_WHEN:
        return True
    setting, wanted = ASKED_ONLY_WHEN[name]
    return values.get(setting) == wanted


def help_text(name, values):
    if name == "HOMEPAGE_PORT":
        if not values.get("HOMEPAGE_PORT") and ports.port_in_use(80):
            return "Port 80 is in use on this machine, so a free one is suggested. The page is at http://<machine>:<port>."
        return "The page is at http://<machine>, with :<port> unless the port is 80."
    return HELP.get(name, "")


def normalize(name, value):
    if name == "HOMEPAGE_PORT":
        return value or first_free_landing_page_port()
    if name == "BACKUP_FOLDER" and value.startswith("~"):
        return os.path.expanduser("~") + value[1:]
    return value


def known_time_zone(name):
    try:
        zoneinfo.ZoneInfo(name)
    except (ValueError, OSError, zoneinfo.ZoneInfoNotFoundError):
        return False
    return True


def port_problem(value):
    if not re.fullmatch(r"[0-9]+", value) or not 1 <= int(value) <= 65535:
        return "The landing page port must be a port number, from 1 to 65535."
    if ports.port_in_use(int(value)):
        return f"Port {value} is in use on this machine; choose another, such as {first_free_landing_page_port()}."
    return ""


def problem(name, value):
    if name == "TZ":
        return "" if known_time_zone(value) else f"{value} is not a time zone. Use a name such as Europe/London."
    if name == "HOMEPAGE_PORT":
        return port_problem(value)
    if name in CHOICES:
        choices = [choice for choice, _ in CHOICES[name]]
        return "" if value in choices else f"Choose one of: {' '.join(choices)}."
    if name == "BACKUP_FOLDER":
        return "" if value.startswith("/") else "The backup folder must be an absolute path, such as /mnt/backup/restic."
    if name == "BACKUP_URL":
        return "" if re.match(r"[a-z0-9]+:", value) else "A restic repository starts with its kind, such as sftp: or s3:."
    if name == "PORTAINER_ADMIN_PASSWORD":
        return "" if len(value) >= PORTAINER_PASSWORD_MINIMUM else f"Portainer needs at least {PORTAINER_PASSWORD_MINIMUM} characters."
    return ""


def split_repository(values):
    repository = values.get("RESTIC_REPOSITORY", "")
    if repository.startswith("b2:"):
        bucket, _, folder = repository[len("b2:"):].partition(":")
        values.update(BACKUP_TYPE="b2", B2_BUCKET=bucket, B2_FOLDER=folder)
    elif repository.startswith("/"):
        values.update(BACKUP_TYPE="local", BACKUP_FOLDER=repository)
    elif repository:
        values.update(BACKUP_TYPE="other", BACKUP_URL=repository)


def join_repository(values):
    kind = values.get("BACKUP_TYPE")
    if kind == "b2":
        folder = values.get("B2_FOLDER", "")
        values["RESTIC_REPOSITORY"] = f"b2:{values.get('B2_BUCKET', '')}" + (f":{folder}" if folder else "")
    elif kind == "other":
        values["RESTIC_REPOSITORY"] = values.get("BACKUP_URL", "")
    else:
        values["RESTIC_REPOSITORY"] = values.get("BACKUP_FOLDER", "")


def local_folder_problem(folder):
    try:
        os.makedirs(folder, exist_ok=True)
        with tempfile.NamedTemporaryFile(dir=folder, prefix=".media-server-check."):
            pass
    except OSError:
        return f"{folder} cannot be created or written to by {pwd.getpwuid(os.geteuid()).pw_name}."
    return ""


def b2_problem(values):
    if not shutil.which("restic"):
        return ""
    repository = values["RESTIC_REPOSITORY"]
    env = dict(
        os.environ,
        B2_ACCOUNT_ID=values.get("B2_ACCOUNT_ID", ""),
        B2_ACCOUNT_KEY=values.get("B2_ACCOUNT_KEY", ""),
        RESTIC_PASSWORD=values.get("RESTIC_PASSWORD", ""),
    )
    code, _, errors = commands.captured(["restic", "-r", repository, "cat", "config"], env=env, timeout=backup_check_seconds())
    if code in (0, RESTIC_REPOSITORY_DOES_NOT_EXIST):
        return ""
    if code == RESTIC_WRONG_PASSWORD:
        return f"The restic password does not open the backups already in {repository}."
    last_line = next((line for line in reversed(errors.splitlines()) if line), "")
    return f"Backblaze B2 did not accept these details: {last_line}"


def backup_problem(values):
    kind = values.get("BACKUP_TYPE")
    if kind == "b2":
        return b2_problem(values)
    if kind == "other":
        return ""
    return local_folder_problem(values.get("BACKUP_FOLDER", ""))


def fields_in(section, values):
    return [field for field in FIELDS if field.section == section and applies(field.name, values)]


def sections(values):
    in_order = list(dict.fromkeys(field.section for field in FIELDS))
    return [section for section in in_order if fields_in(section, values)]
