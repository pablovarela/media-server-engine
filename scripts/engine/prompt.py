import getpass
import io
import os
import sys


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
        return getpass.getpass(prompt="", stream=sys.stderr)
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
