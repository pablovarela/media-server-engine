import getpass
import os
import sys


def read(secret=False):
    if secret and sys.stdin.isatty():
        return getpass.getpass(prompt="", stream=sys.stderr)
    typed = sys.stdin.readline()
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
