import importlib
import io
import json
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
        return [f"{r.method} {r.path}" for r in self.requests if r.method != "GET" and host in (None, r.host)]

    def body(self, method, path, host=None):
        return next(r.body for r in self.requests if (r.method, r.path) == (method, path) and host in (None, r.host))


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
