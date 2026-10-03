import json
import os
import re
from secrets import token_hex

from engine import commands, installation


def write_private(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.fchmod(descriptor, 0o600)
    with os.fdopen(descriptor, "w") as file:
        file.write(text)


def private_directory(path):
    previous = os.umask(0o077)
    try:
        os.makedirs(path, exist_ok=True)
    finally:
        os.umask(previous)


def decrypted(name):
    return commands.output(["sops", "decrypt", "--output-type", "dotenv", os.path.join(installation.config_dir(), "secrets", name)])


def dotenv(text):
    return dict(line.partition("=")[::2] for line in text.splitlines() if line)


def gluetun_control_key():
    path = os.path.join(installation.data_dir(), "volumes", ".wiring", "gluetun-control.key")
    if not (os.path.isfile(path) and os.path.getsize(path)):
        private_directory(os.path.dirname(path))
        write_private(path, token_hex(16) + "\n")
    with open(path) as key:
        return key.read().strip()


def configarr_secrets(apps):
    try:
        with open(os.path.join(installation.config_dir(), "configarr", "config.yml")) as config:
            wanted = set(re.findall(r"!secret\s+([A-Za-z0-9_]+)", config.read()))
    except FileNotFoundError:
        wanted = set()
    return "".join(f"{key}: {json.dumps(value)}\n" for key, value in dotenv(apps).items() if key in wanted)


def decrypt_all():
    secrets_dir = os.path.join(installation.engine_dir(), ".secrets")
    private_directory(secrets_dir)
    private_directory(os.path.join(secrets_dir, "configarr"))
    write_private(os.path.join(secrets_dir, "vpn.env"), decrypted("vpn.sops.env"))
    apps = decrypted("apps.sops.env")
    write_private(os.path.join(secrets_dir, "apps.env"), apps)
    healthchecks = os.path.join(installation.config_dir(), "secrets", "healthchecks.sops.env")
    write_private(os.path.join(secrets_dir, "healthchecks.env"), decrypted("healthchecks.sops.env") if os.path.isfile(healthchecks) else "")
    control = f'HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={{"auth":"apikey","apikey":"{gluetun_control_key()}"}}\n'
    write_private(os.path.join(secrets_dir, "gluetun.env"), control)
    write_private(os.path.join(secrets_dir, "configarr", "secrets.yml"), configarr_secrets(apps))
    keys = dotenv(apps)
    for app in ("sonarr", "radarr", "prowlarr"):
        upper = app.upper()
        write_private(os.path.join(secrets_dir, f"{app}.env"), f"{upper}__AUTH__APIKEY={keys.get(f'{upper}_API_KEY', '')}\n")
    write_private(os.path.join(secrets_dir, "portainer_admin"), keys.get("PORTAINER_ADMIN_PASSWORD", ""))
