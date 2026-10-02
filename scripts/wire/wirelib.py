import hashlib
import http.client
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

import yaml

ENGINE_DIR = os.environ["ENGINE_DIR"]
CONFIG_DIR = os.environ["CONFIG_DIR"]
DATA_DIR = os.environ["DATA_DIR"]
DRY_RUN = bool(os.environ.get("WIRE_DRY_RUN"))
WIRING_STATE_DIR = os.path.join(DATA_DIR, "volumes", ".wiring")


class StillStarting(Exception):
    def __init__(self, message, maybe_acted):
        super().__init__(message)
        self.maybe_acted = maybe_acted


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


SECRET_FIELD = re.compile(r"key|password|token|secret", re.IGNORECASE)
QUOTED = re.compile(r'"([^"]{6,})"')
secrets_sent = set()


def remember_secrets_sent(headers, data):
    for value in headers.values():
        secrets_sent.add(value)
        secrets_sent.update(QUOTED.findall(value))
    pending = [data]
    while pending:
        item = pending.pop()
        if isinstance(item, dict):
            for field, value in item.items():
                if isinstance(value, str) and SECRET_FIELD.search(field):
                    secrets_sent.add(value)
                else:
                    pending.append(value)
        elif isinstance(item, list):
            pending.extend(item)


def without_secrets(text):
    try:
        known = set(app_secrets().values())
    except FileNotFoundError:
        known = set()
    for secret in sorted((s for s in known | secrets_sent if len(s) >= 6), key=len, reverse=True):
        text = re.sub(rf"(?<!\w){re.escape(secret)}(?!\w)", "<hidden>", text)
    return text


class Api:
    def __init__(self, app, base_url, headers):
        self.app = app
        self.base_url = base_url.rstrip("/")
        self.headers = headers

    def request(self, method, path, body=None, form=None):
        attempts = int(os.environ.get("WIRE_REQUEST_ATTEMPTS", "5"))
        for attempt in range(1, attempts + 1):
            try:
                return self.send(method, path, body, form)
            except StillStarting as starting:
                if attempt == attempts or (starting.maybe_acted and method != "GET"):
                    raise WiringError(str(starting)) from None
                time.sleep(float(os.environ.get("WIRE_REQUEST_RETRY_SECONDS", "3")))

    def send(self, method, path, body, form):
        if form is not None:
            data, content_type = urllib.parse.urlencode(form, doseq=True).encode(), "application/x-www-form-urlencoded"
        else:
            data, content_type = (None if body is None else json.dumps(body).encode()), "application/json"
        remember_secrets_sent(self.headers, body if form is None else form)
        request = urllib.request.Request(self.base_url + path, data=data, method=method)
        for key, value in self.headers.items():
            request.add_header(key, value)
        if data is not None:
            request.add_header("Content-Type", content_type)
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                payload = response.read()
        except urllib.error.HTTPError as error:
            detail = without_secrets(error.read().decode(errors="replace"))[:300]
            message = f"{method} {path} answered {error.code}: {detail}"
            if error.code == 503:
                raise StillStarting(message, maybe_acted=False) from None
            raise WiringError(message) from None
        except urllib.error.URLError as error:
            message = f"{method} {path} failed: {error.reason}"
            if isinstance(error.reason, ConnectionRefusedError):
                raise StillStarting(message, maybe_acted=False) from None
            if isinstance(error.reason, ConnectionResetError):
                raise StillStarting(message, maybe_acted=True) from None
            raise WiringError(message) from None
        except (ConnectionResetError, http.client.RemoteDisconnected) as error:
            raise StillStarting(f"{method} {path} failed: {error}", maybe_acted=True) from None
        except (http.client.HTTPException, OSError) as error:
            raise WiringError(f"{method} {path} failed: {error}") from None
        return json.loads(payload) if payload.strip() else None

    def get(self, path):
        return self.request("GET", path)

    def write(self, method, path, body=None, form=None):
        if DRY_RUN:
            return body
        return self.request(method, path, body, form)


def run(app, wire):
    try:
        wire()
    except WiringError as error:
        print(without_secrets(f"{app}: {error}"), file=sys.stderr)
        sys.exit(1)
