import getpass
import io
import os
import sys

import pytest

from conftest import fresh_engine


@pytest.fixture
def prompt(monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ.pop("PROMPT_INPUT_ENDED", None)
    return fresh_engine("engine.prompt")


def typed(monkeypatch, text, terminal=False):
    stdin = io.StringIO(text)
    stdin.isatty = lambda: terminal
    monkeypatch.setattr(sys, "stdin", stdin)


def test_a_question_shows_its_default_and_an_empty_answer_takes_it(prompt, monkeypatch, capsys):
    typed(monkeypatch, "\n")
    assert prompt.ask("Make this machine the main? (y/n)", "n") == "n"
    assert capsys.readouterr().err == "Make this machine the main? (y/n) [n]: \n"


def test_a_typed_answer_wins_over_the_default(prompt, monkeypatch):
    typed(monkeypatch, "y\n")
    assert prompt.ask("Make this machine the main? (y/n)", "n") == "y"


def test_answers_that_ran_out_are_remembered(prompt, monkeypatch):
    typed(monkeypatch, "")
    assert prompt.ask("Time zone", "Europe/London") == "Europe/London"
    assert os.environ["PROMPT_INPUT_ENDED"] == "1"


def test_a_secret_is_read_without_echo_on_a_terminal(prompt, monkeypatch, capsys):
    typed(monkeypatch, "", terminal=True)
    monkeypatch.setattr(getpass, "getpass", lambda prompt="", stream=None: "AGE-SECRET-KEY-1")
    assert prompt.secret("Paste the secrets key: ") == "AGE-SECRET-KEY-1"
    assert capsys.readouterr().err == "Paste the secrets key: "


def test_a_secret_from_a_pipe_is_read_as_a_line(prompt, monkeypatch, capsys):
    typed(monkeypatch, "AGE-SECRET-KEY-1\n")
    assert prompt.secret("Paste the secrets key: ") == "AGE-SECRET-KEY-1"
    assert capsys.readouterr().err == "Paste the secrets key: \n"
