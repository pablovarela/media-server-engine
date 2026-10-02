import importlib
import json
import sys
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
