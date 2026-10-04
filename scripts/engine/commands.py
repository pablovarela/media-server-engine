import subprocess
import sys

TIMED_OUT = 124


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


PASSED_TO_CHILDREN = set()
RUNNING = []
STOP_ONCE_FINISHED = []


def forward(signum):
    if not RUNNING:
        return False
    RUNNING[-1].send_signal(signum)
    STOP_ONCE_FINISHED.append(128 + signum)
    return True


def finished(args, child, input=None, timeout=None):
    RUNNING.append(child)
    try:
        stdout, stderr = child.communicate(input, timeout=timeout)
        returncode = child.returncode
    except subprocess.TimeoutExpired:
        child.kill()
        stdout, stderr = child.communicate()
        returncode = TIMED_OUT
    except KeyboardInterrupt:
        child.wait()
        raise
    finally:
        RUNNING.remove(child)
    if STOP_ONCE_FINISHED:
        raise SystemExit(STOP_ONCE_FINISHED.pop())
    return subprocess.CompletedProcess(args, returncode, stdout, stderr)


def completed(args, input=None, timeout=None, **options):
    if PASSED_TO_CHILDREN:
        options["pass_fds"] = tuple(sorted(PASSED_TO_CHILDREN))
    sys.stdout.flush()
    sys.stderr.flush()
    try:
        if input is not None:
            options["stdin"] = subprocess.PIPE
        result = finished(args, subprocess.Popen(args, text=True, **options), input, timeout)
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


def run(args, env=None, check=True, discard_output=False, input=None):
    result = completed(args, input=input, env=env, stdout=subprocess.DEVNULL if discard_output else None)
    return checked(result, check).returncode


def output(args, env=None, check=True, discard_errors=False, input=None):
    result = completed(args, input=input, env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL if discard_errors else None)
    return checked(result, check).stdout


def combined(args, env=None):
    result = completed(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    return result.returncode, result.stdout


def captured(args, env=None, input=None, timeout=None):
    result = completed(args, input=input, timeout=timeout, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return result.returncode, result.stdout, result.stderr


def dialog(args):
    result = completed(args, stderr=subprocess.PIPE)
    return result.returncode, result.stderr


def quiet(args):
    return completed(args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode


def succeeds(args):
    return quiet(args) == 0
