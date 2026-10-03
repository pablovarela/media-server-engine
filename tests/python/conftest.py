import importlib
import io
import json
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import pytest

REPO = Path(__file__).resolve().parents[2]


@pytest.fixture
def dirs(tmp_path, monkeypatch):
    paths = SimpleNamespace(engine=tmp_path / "engine", config=tmp_path / "config", data=tmp_path / "data")
    for path in vars(paths).values():
        path.mkdir()
    monkeypatch.setenv("ENGINE_DIR", str(paths.engine))
    monkeypatch.setenv("CONFIG_DIR", str(paths.config))
    monkeypatch.setenv("DATA_DIR", str(paths.data))
    return paths


def fresh_import(name):
    sys.modules.pop(name, None)
    return importlib.import_module(name)


def answer(body):
    response = mock.MagicMock()
    response.__enter__.return_value.read.return_value = json.dumps(body).encode()
    return response


@pytest.fixture
def urlopen(monkeypatch):
    opened = mock.Mock(side_effect=OSError("no answer was set up for this request"))
    monkeypatch.setattr(urllib.request, "urlopen", opened)
    return opened


def http_error(code, body=""):
    return lambda: urllib.error.HTTPError("http://app", code, "error", {}, io.BytesIO(body.encode()))


def reset():
    return lambda: ConnectionResetError("connection reset by peer")


def refused():
    return lambda: urllib.error.URLError(ConnectionRefusedError("connection refused"))


class Http:
    def __init__(self):
        self.answers = {}
        self.requests = []

    def on(self, method, path, *outcomes):
        self.answers[(method, path)] = list(outcomes) or [None]

    def __call__(self, request, timeout=None):
        url = urllib.parse.urlsplit(request.full_url)
        path = url.path + (f"?{url.query}" if url.query else "")
        method = request.get_method()
        self.requests.append(SimpleNamespace(method=method, host=url.netloc, path=path, body=body_of(request), headers=dict(request.header_items())))
        outcomes = self.answers.get((method, f"{url.scheme}://{url.netloc}{path}")) or self.answers.get((method, path))
        if outcomes is None:
            raise AssertionError(f"unexpected request: {method} {url.netloc}{path}")
        outcome = outcomes.pop(0) if len(outcomes) > 1 else outcomes[0]
        if callable(outcome):
            raise outcome()
        return answer(outcome)

    def writes(self, host=None):
        return [f"{r.method} {r.path}" for r in self.requests_to(host) if r.method != "GET"]

    def body(self, method, path, host=None):
        return next(r.body for r in self.requests_to(host) if (r.method, r.path) == (method, path))

    def requests_to(self, host):
        hosts = {r.host for r in self.requests}
        if host is not None and host not in hosts:
            raise AssertionError(f"no request was made to {host}; requests went to {sorted(hosts)}")
        return [r for r in self.requests if host in (None, r.host)]


def body_of(request):
    if request.data is None:
        return None
    if request.get_header("Content-type") == "application/x-www-form-urlencoded":
        fields = urllib.parse.parse_qs(request.data.decode(), keep_blank_values=True)
        return {field: values[0] if len(values) == 1 else values for field, values in fields.items()}
    return json.loads(request.data)


@pytest.fixture
def http(monkeypatch):
    mock_http = Http()
    monkeypatch.setattr(urllib.request, "urlopen", mock_http)
    return mock_http


@pytest.fixture
def wiring(dirs, monkeypatch):
    monkeypatch.setenv("WIRE_REQUEST_RETRY_SECONDS", "0")
    (dirs.engine / ".secrets").mkdir()

    def load(module, secrets="", dry_run=False):
        (dirs.engine / ".secrets" / "apps.env").write_text(secrets)
        if dry_run:
            monkeypatch.setenv("WIRE_DRY_RUN", "1")
        else:
            monkeypatch.delenv("WIRE_DRY_RUN", raising=False)
        sys.modules.pop("wirelib", None)
        return fresh_import(module)

    return load


def done(stdout="", stderr="", returncode=0, then=None):
    return SimpleNamespace(stdout=stdout, stderr=stderr, returncode=returncode, then=then)


def matches(words, args):
    position = 0
    for arg in args:
        if position < len(words) and (arg == words[position] or arg.endswith("/" + words[position])):
            position += 1
    return position == len(words)


class Commands:
    def __init__(self):
        self.answers = []
        self.ran = []

    def on(self, words, *outcomes):
        self.answers.insert(0, (list(words), list(outcomes) or [done()]))

    def __call__(self, args, **options):
        args = [str(arg) for arg in args]
        self.ran.append(SimpleNamespace(args=args, env=options.get("env")))
        outcomes = next((outcomes for words, outcomes in self.answers if matches(words, args)), None)
        if outcomes is None:
            raise AssertionError(f"unexpected command: {' '.join(args)}")
        outcome = outcomes.pop(0) if len(outcomes) > 1 else outcomes[0]
        if outcome.then:
            outcome.then()
        return completed(args, outcome, options)

    def did(self, *words):
        return any(matches(words, command.args) for command in self.ran)

    def count(self, *words):
        return sum(matches(words, command.args) for command in self.ran)

    def index(self, *words):
        return next(i for i, command in enumerate(self.ran) if matches(words, command.args))

    def env_of(self, *words):
        return next(command.env for command in self.ran if matches(words, command.args))


def completed(args, outcome, options):
    if options.pop("capture_output", False):
        options.update(stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    captured = options.get("stdout") == subprocess.PIPE
    merged = options.get("stderr") == subprocess.STDOUT
    if not captured and options.get("stdout") != subprocess.DEVNULL:
        sys.stdout.write(outcome.stdout)
    if not merged and options.get("stderr") not in (subprocess.DEVNULL, subprocess.PIPE):
        sys.stderr.write(outcome.stderr)
    stdout = outcome.stdout + (outcome.stderr if merged else "") if captured else None
    stderr = outcome.stderr if options.get("stderr") == subprocess.PIPE else None
    return subprocess.CompletedProcess(args, outcome.returncode, stdout, stderr)


@pytest.fixture
def commands(monkeypatch):
    mock_commands = Commands()
    monkeypatch.setattr(subprocess, "run", mock_commands)
    return mock_commands


def fresh_engine(name):
    for module in [module for module in sys.modules if module == "engine" or module.startswith("engine.")]:
        sys.modules.pop(module)
    return importlib.import_module(name)
