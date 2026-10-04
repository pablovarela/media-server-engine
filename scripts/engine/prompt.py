import getpass
import io
import os
import sys
from secrets import token_urlsafe


def line():
    try:
        descriptor = sys.stdin.fileno()
    except (AttributeError, ValueError, io.UnsupportedOperation):
        return sys.stdin.readline()
    collected = b""
    while not collected.endswith(b"\n"):
        byte = os.read(descriptor, 1)
        if not byte:
            break
        collected += byte
    return collected.decode(errors="replace")


def read(secret=False):
    if secret and sys.stdin.isatty():
        try:
            return getpass.getpass(prompt="", stream=sys.stderr)
        except EOFError:
            os.environ["PROMPT_INPUT_ENDED"] = "1"
            return ""
    typed = line()
    if not typed:
        os.environ["PROMPT_INPUT_ENDED"] = "1"
    if not sys.stdin.isatty():
        print(file=sys.stderr)
    return typed.rstrip("\n")


def ask(question, default=""):
    print(f"{question} [{default}]: ", end="", file=sys.stderr, flush=True)
    return read() or default


def secret(text):
    print(text, end="", file=sys.stderr, flush=True)
    return read(secret=True)


def mask(value):
    if not value:
        return "not set"
    return f"set, ends …{value[-3:]}" if len(value) > 3 else "set"


def generated_password():
    return token_urlsafe(24)


def ask_secret(label, current=""):
    print(f"{label} [{mask(current)}]: ", end="", file=sys.stderr, flush=True)
    return read(secret=True) or current


def ask_password(label, current=""):
    shown = mask(current) if current else "Enter generates one"
    print(f"{label} [{shown}]: ", end="", file=sys.stderr, flush=True)
    return read(secret=True) or current or generated_password()
