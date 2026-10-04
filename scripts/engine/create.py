import os
import re
import shutil
import sys
import tempfile

from engine import commands, configure, installation, program, prompt

SOPS_CONFIG = "creation_rules:\n  - path_regex: (^|/)secrets/[^/]+\\.sops\\.env$\n    age: {}\n"
SCRIPTS_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def engine_repository():
    remote = commands.output(["git", "-C", installation.engine_dir(), "remote", "get-url", "origin"]).strip()
    return re.sub(r"\.git$", "", re.sub(r"^.*github\.com[:/]", "", remote))


def keys_file():
    return os.environ["SOPS_AGE_KEY_FILE"]


def ends_mid_line(path):
    if not os.path.isfile(path) or os.path.getsize(path) == 0:
        return False
    with open(path, "rb") as file:
        file.seek(-1, os.SEEK_END)
        return file.read(1) != b"\n"


def add_secret_key(secret):
    folder = os.path.dirname(keys_file())
    os.makedirs(folder, exist_ok=True)
    os.chmod(folder, 0o700)
    separator = b"\n" if ends_mid_line(keys_file()) else b""
    descriptor = os.open(keys_file(), os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(descriptor, "ab") as keys:
        keys.write(separator + secret.encode() + b"\n")
    os.chmod(keys_file(), 0o600)


def new_secrets_key():
    with tempfile.TemporaryDirectory() as scratch:
        generated = os.path.join(scratch, "key")
        if commands.quiet(["age-keygen", "-o", generated]) != 0:
            raise commands.CommandFailed(1)
        with open(generated) as key:
            lines = key.read().splitlines()
    public = next(line[len("# public key: "):] for line in lines if line.startswith("# public key: "))
    secret = next(line for line in lines if line.startswith("AGE-SECRET-KEY-"))
    add_secret_key(secret)
    return public, secret


def show_secrets_key(name, secret):
    print(
        f"The secrets key for {name}. Save this line in your password manager now; without it nothing in {name}'s config can be decrypted:\n\n"
        f"{secret}\n\nPress Enter once it is saved. ",
        end="", file=sys.stderr, flush=True,
    )
    prompt.line()


def is_key_of(line, public):
    key = line.strip()
    if not key.startswith(b"AGE-SECRET-KEY-"):
        return False
    return commands.output(["age-keygen", "-y"], input=key.decode() + "\n", check=False, discard_errors=True).strip() == public


def remove_secrets_key(public):
    with open(keys_file(), "rb") as keys:
        lines = keys.read().splitlines(keepends=True)
    kept = b"".join(line for line in lines if not is_key_of(line, public))
    descriptor, staged = tempfile.mkstemp(prefix=os.path.basename(keys_file()) + ".", dir=os.path.dirname(keys_file()))
    with os.fdopen(descriptor, "wb") as file:
        file.write(kept)
    os.chmod(staged, 0o600)
    os.replace(staged, keys_file())


def fill_template(repository):
    engine, config = installation.engine_dir(), installation.config_dir()
    shutil.copytree(os.path.join(engine, "config-template"), config, dirs_exist_ok=True)
    os.makedirs(os.path.join(config, "homepage"), exist_ok=True)
    homepage = os.path.join(engine, "homepage")
    for name in os.listdir(homepage):
        if os.path.isfile(os.path.join(homepage, name)):
            shutil.copy(os.path.join(homepage, name), os.path.join(config, "homepage", name))
    for folder, _, files in os.walk(config):
        for name in files:
            path = os.path.join(folder, name)
            with open(path, "rb") as file:
                content = file.read()
            if b"ENGINE_REPOSITORY" in content:
                with open(path, "wb") as file:
                    file.write(content.replace(b"ENGINE_REPOSITORY", repository.encode()))


def pin_engine_version():
    version = commands.output(["git", "-C", installation.engine_dir(), "describe", "--tags", "--exact-match"], check=False, discard_errors=True).strip()
    with open(os.path.join(installation.config_dir(), "engine.env"), "w") as pin:
        pin.write(f"ENGINE_VERSION={version or 'local'}\n")


def undo(name, public, created_data_dir):
    if public:
        remove_secrets_key(public)
        print(f"The secrets key shown for {name} was removed and is no longer needed; delete it from your password manager.", file=sys.stderr)
    shutil.rmtree(installation.config_dir(), ignore_errors=True)
    if created_data_dir:
        shutil.rmtree(installation.data_dir(), ignore_errors=True)
    if os.environ.get("CREATED_INSTALL_DIR"):
        shutil.rmtree(os.environ["CREATED_INSTALL_DIR"], ignore_errors=True)
    print(f"Stopped before {name}'s settings were saved, so nothing was kept. Run make create-installation NAME={name} again.", file=sys.stderr)


def create(argv):
    name = (argv[0] if argv else "") or os.environ.get("NAME", "")
    if not name:
        raise commands.Stop("usage: make create-installation NAME=<installation name>")
    installation.require_valid_name(name)
    os.environ.setdefault("SOPS_AGE_KEY_FILE", os.path.join(os.environ["HOME"], ".config", "sops", "age", "keys.txt"))
    installation.move_into(name, "create-installation")
    repository = engine_repository()
    owner = os.environ.get("GITHUB_OWNER") or repository.split("/", 1)[0]
    config_repository = f"{owner}/media-server-config-{name}"
    config, data = installation.config_dir(), installation.data_dir()
    if commands.quiet(["gh", "auth", "status"]) == 0 and commands.quiet(["gh", "repo", "view", config_repository]) == 0:
        raise commands.Stop(f"{config_repository} already exists; use make join-installation NAME={name}")
    if os.path.exists(config) and os.listdir(config):
        raise commands.Stop(f"{config} is not empty")
    created_data_dir = not os.path.exists(data)
    public, saved = "", False
    try:
        commands.run([os.path.join(SCRIPTS_DIR, "bootstrap.sh")])
        os.makedirs(config, exist_ok=True)
        os.makedirs(data, exist_ok=True)
        public, secret = new_secrets_key()
        show_secrets_key(name, secret)
        fill_template(repository)
        pin_engine_version()
        with open(os.path.join(config, ".sops.yaml"), "w") as sops:
            sops.write(SOPS_CONFIG.format(public))
        commands.run(["git", "-C", config, "init", "-q", "-b", "main"])
        configure.configure(name, from_create=True)
        saved = True
        commands.run([installation.engine_run(), "setup-machine"])
    except BaseException as error:
        stopped = isinstance(error, (commands.Stop, OSError, ValueError))
        if stopped:
            print(error.text("create-installation") if isinstance(error, commands.Stop) else f"create-installation: {error}", file=sys.stderr)
        with program.finishing():
            if saved:
                place = os.path.dirname(os.path.abspath(installation.engine_dir()))
                print(f"{name}'s settings are saved in {config}. Finish setting up this machine with: cd {place} && make setup-machine", file=sys.stderr)
            else:
                undo(name, public, created_data_dir)
        if stopped:
            raise commands.CommandFailed(1) from None
        raise


def main(argv):
    return program.run("create-installation", create, argv)
