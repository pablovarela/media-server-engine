import hashlib
import json
import os
import sys
import urllib.error
import urllib.request

import yaml

ENGINE_DIR = os.environ["ENGINE_DIR"]
CONFIG_DIR = os.environ["CONFIG_DIR"]
DATA_DIR = os.environ["DATA_DIR"]
DRY_RUN = bool(os.environ.get("WIRE_DRY_RUN"))
WIRING_STATE_DIR = os.path.join(DATA_DIR, "volumes", ".wiring")


class WiringError(Exception):
    pass


def app_secrets():
    secrets = {}
    with open(os.path.join(ENGINE_DIR, ".secrets", "apps.env")) as env:
        for line in env:
            key, _, value = line.rstrip("\n").partition("=")
            if key:
                secrets[key] = value
    return secrets


def declared(name):
    with open(os.path.join(CONFIG_DIR, name)) as config:
        return yaml.safe_load(config) or {}


def report(app, change):
    prefix = "(dry run) " if DRY_RUN else ""
    print(f"{prefix}{app}: {change}", flush=True)


def fingerprint(*values):
    return hashlib.sha256("\0".join(values).encode()).hexdigest()


def remembered(name):
    try:
        with open(os.path.join(WIRING_STATE_DIR, name)) as state:
            return state.read().strip()
    except FileNotFoundError:
        return None


def remember(name, value):
    if DRY_RUN:
        return
    os.makedirs(WIRING_STATE_DIR, exist_ok=True)
    path = os.path.join(WIRING_STATE_DIR, name)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as state:
        state.write(value + "\n")


class Api:
    def __init__(self, app, base_url, headers):
        self.app = app
        self.base_url = base_url.rstrip("/")
        self.headers = headers

    def request(self, method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.base_url + path, data=data, method=method)
        for key, value in self.headers.items():
            request.add_header(key, value)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                payload = response.read()
        except urllib.error.HTTPError as error:
            detail = error.read().decode(errors="replace")[:300]
            raise WiringError(f"{method} {path} answered {error.code}: {detail}") from None
        except urllib.error.URLError as error:
            raise WiringError(f"{method} {path} failed: {error.reason}") from None
        return json.loads(payload) if payload.strip() else None

    def get(self, path):
        return self.request("GET", path)

    def write(self, method, path, body=None):
        if DRY_RUN:
            return body
        return self.request(method, path, body)


def run(app, wire):
    try:
        wire()
    except WiringError as error:
        print(f"{app}: {error}", file=sys.stderr)
        sys.exit(1)
