import getpass
import io
import os
import re
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



def test_an_answer_read_from_a_pipe_leaves_the_next_lines_for_the_programs_started_after(prompt, monkeypatch):
    read_end, write_end = os.pipe()
    os.write(write_end, b"n\nnext answer\n")
    os.close(write_end)
    stdin = os.fdopen(read_end)
    monkeypatch.setattr(sys, "stdin", stdin)
    try:
        assert prompt.ask("Make this machine the main? (y/n)", "y") == "n"
        assert os.read(read_end, 100) == b"next answer\n"
    finally:
        stdin.close()



def test_ctrl_d_at_a_hidden_prompt_is_an_empty_answer(prompt, monkeypatch):
    typed(monkeypatch, "", terminal=True)

    def ended(prompt="", stream=None):
        raise EOFError

    monkeypatch.setattr(getpass, "getpass", ended)
    assert prompt.secret("Paste the secrets key: ") == ""
    assert os.environ["PROMPT_INPUT_ENDED"] == "1"


def test_a_secret_question_shows_only_the_end_of_the_current_value_and_enter_keeps_it(prompt, monkeypatch, capsys):
    typed(monkeypatch, "\n")
    assert prompt.ask_secret("B2 application key", "K005supersecretvalueXYZ") == "K005supersecretvalueXYZ"
    assert capsys.readouterr().err == "B2 application key [set, ends …XYZ]: \n"


def test_a_secret_question_says_when_nothing_is_set_and_takes_a_typed_value(prompt, monkeypatch, capsys):
    typed(monkeypatch, "new\n")
    assert prompt.ask_secret("B2 application key", "") == "new"
    assert "B2 application key [not set]: " in capsys.readouterr().err


def test_a_password_question_generates_one_on_enter_when_none_is_set(prompt, monkeypatch, capsys):
    typed(monkeypatch, "\n")
    password = prompt.ask_password("Jellyfin admin password", "")
    assert re.fullmatch(r"[A-Za-z0-9_-]{32}", password)
    assert capsys.readouterr().err == "Jellyfin admin password [Enter generates one]: \n"


def test_a_password_question_keeps_the_current_password_on_enter(prompt, monkeypatch, capsys):
    typed(monkeypatch, "\n")
    assert prompt.ask_password("Jellyfin admin password", "current-one") == "current-one"
    assert capsys.readouterr().err == "Jellyfin admin password [set, ends …one]: \n"


def test_a_typed_password_wins(prompt, monkeypatch):
    typed(monkeypatch, "typed-one\n")
    assert prompt.ask_password("Jellyfin admin password", "current-one") == "typed-one"


def test_generated_passwords_differ(prompt):
    assert prompt.generated_password() != prompt.generated_password()


def test_a_mask_shows_only_the_last_three_characters(prompt):
    assert (prompt.mask("abcdefgh"), prompt.mask("")) == ("set, ends …fgh", "not set")


def test_a_secret_of_three_characters_or_fewer_is_only_said_to_be_set(prompt):
    assert (prompt.mask("ab"), prompt.mask("abc"), prompt.mask("abcd")) == ("set", "set", "set, ends …bcd")
