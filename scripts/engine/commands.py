import subprocess
import sys


class Stop(Exception):
    def __init__(self, message, prefixed=True):
        super().__init__(message)
        self.prefixed = prefixed

    def text(self, program):
        return f"{program}: {self}" if self.prefixed else str(self)


class CommandFailed(Exception):
    def __init__(self, returncode):
        super().__init__(f"exited with {returncode}")
        self.returncode = returncode


def completed(args, **options):
    sys.stdout.flush()
    sys.stderr.flush()
    try:
        result = subprocess.run(args, text=True, **options)
    except FileNotFoundError:
        if options.get("stderr") is not subprocess.DEVNULL:
            print(f"{args[0]}: command not found", file=sys.stderr)
        captured_stdout = "" if options.get("stdout") == subprocess.PIPE else None
        captured_stderr = "" if options.get("stderr") == subprocess.PIPE else None
        return subprocess.CompletedProcess(args, 127, captured_stdout, captured_stderr)
    if result.returncode < 0:
        result.returncode = 128 - result.returncode
    return result


def checked(result, check):
    if check and result.returncode != 0:
        raise CommandFailed(result.returncode)
    return result


def run(args, env=None, check=True, discard_output=False):
    result = completed(args, env=env, stdout=subprocess.DEVNULL if discard_output else None)
    return checked(result, check).returncode


def output(args, env=None, check=True, discard_errors=False):
    result = completed(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL if discard_errors else None)
    return checked(result, check).stdout


def combined(args, env=None):
    result = completed(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    return result.returncode, result.stdout


def captured(args, env=None):
    result = completed(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return result.returncode, result.stdout, result.stderr


def quiet(args):
    return completed(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode


def succeeds(args):
    return quiet(args) == 0
