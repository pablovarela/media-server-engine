import importlib
import io
import json
import os
import shlex
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


def through_sops(words):
    while len(words) > 1 and words[0].endswith("sops") and words[1] == "exec-env":
        assert len(words) == 4, f"sops exec-env takes one command after its file: {words}"
        words = shlex.split(words[3])
    return words


def real():
    return SimpleNamespace(real=True, then=None)


def replied(reply):
    return SimpleNamespace(reply=reply, then=None)


def timed_out(stdout="", stderr=""):
    return SimpleNamespace(stdout=stdout, stderr=stderr, returncode=-9, then=None, timed_out=True)


def whiptail(commands, *answers):
    def more_than_scripted():
        raise AssertionError("whiptail was asked more than scripted")

    scripted = [done(stderr=answer.partition("|")[2], returncode=int(answer.partition("|")[0])) for answer in answers]
    commands.on(["whiptail"], *scripted, done(then=more_than_scripted))
    commands.on(["whiptail", "--msgbox"])


def whiptail_screens(commands):
    return [" ".join(command.args) for command in commands.ran if command.args[0] == "whiptail"]


def guided_answers(folder):
    return [
        "0|Europe", "0|London", "0|local", "0|admin", "0|local", f"0|{folder}", "0|restic-typed", "0|protonvpn", "0|vpn-user", "0|vpn-password",
        "0|Ireland", "0|", "0|", "0|", "0|jelly-typed", "0|", "0|portainer-pass-long", "0|en", "0|",
    ]


class Process:
    def __init__(self, args, outcome, options):
        self.args = args
        self.outcome = outcome
        self.options = options
        self.returncode = None
        self.signals = []
        self.input = None
        self.killed = False

    def communicate(self, input=None, timeout=None):
        self.input = input
        if getattr(self.outcome, "timed_out", False) and timeout is not None and not self.killed:
            raise subprocess.TimeoutExpired(self.args, timeout)
        if self.outcome.then:
            self.outcome.then()
        outcome = done(stdout=self.outcome.reply(self.args, input)) if hasattr(self.outcome, "reply") else self.outcome
        result = completed(self.args, outcome, self.options)
        self.returncode = result.returncode
        return result.stdout, result.stderr

    def wait(self):
        return self.returncode

    def kill(self):
        self.killed = True

    def send_signal(self, signum):
        self.signals.append(signum)


class Commands:
    def __init__(self, start):
        self.start = start
        self.answers = []
        self.ran = []

    def on(self, words, *outcomes):
        self.answers.insert(0, (list(words), list(outcomes) or [done()]))

    def __call__(self, args, **options):
        args = [str(arg) for arg in args]
        command = SimpleNamespace(args=args, env=options.get("env"), pass_fds=options.get("pass_fds", ()), process=None)
        self.ran.append(command)
        outcomes = next((outcomes for words, outcomes in self.answers if matches(words, args)), None)
        if outcomes is None:
            raise AssertionError(f"unexpected command: {' '.join(args)}")
        outcome = outcomes.pop(0) if len(outcomes) > 1 else outcomes[0]
        command.process = self.start(args, **options) if getattr(outcome, "real", False) else Process(args, outcome, options)
        return command.process

    def did(self, *words):
        return any(matches(words, command.args) for command in self.ran)

    def count(self, *words):
        return sum(matches(words, command.args) for command in self.ran)

    def index(self, *words):
        return next(i for i, command in enumerate(self.ran) if matches(words, command.args))

    def env_of(self, *words):
        return next(command.env for command in self.ran if matches(words, command.args))

    def input_to(self, *words):
        return [command.process.input for command in self.ran if matches(words, command.args)]

    def signals_to(self, *words):
        return [signum for command in self.ran if matches(words, command.args) for signum in command.process.signals]


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
    mock_commands = Commands(subprocess.Popen)
    monkeypatch.setattr(subprocess, "Popen", mock_commands)
    return mock_commands


def fresh_engine(name):
    for module in [module for module in sys.modules if module == "engine" or module.startswith("engine.")]:
        sys.modules.pop(module)
    return importlib.import_module(name)


@pytest.fixture
def installed(dirs, commands, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    (dirs.config / "installation.env").write_text("INSTALLATION_NAME=testinst\n")
    os.environ.update(INSTALLATION_NAME="testinst", HEALTHCHECKS_PING_KEY="pk")
    commands.on(["bash", "-c"], real())
    return dirs
