import os

from engine import commands, create, deploy_keys, installation, program, prompt

SCRIPTS_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def secrets_key_works():
    return commands.quiet(["sops", "decrypt", os.path.join(installation.config_dir(), "secrets", "vpn.sops.env")]) == 0


def ensure_secrets_key(name, config_repository):
    if secrets_key_works():
        return
    key = "".join(prompt.secret(f"Paste the secrets key for {name} (the AGE-SECRET-KEY-... line): ").split())
    if commands.captured(["age-keygen", "-y"], input=key + "\n")[0] != 0:
        raise commands.Stop("that is not an age secrets key (the line starts with AGE-SECRET-KEY-); nothing was changed")
    folder = os.path.dirname(create.keys_file())
    os.makedirs(folder, exist_ok=True)
    os.chmod(folder, 0o700)
    previous = None
    if os.path.isfile(create.keys_file()):
        with open(create.keys_file(), "rb") as keys:
            previous = keys.read()
    create.add_secret_key(key)
    if not secrets_key_works():
        if previous is None:
            os.remove(create.keys_file())
        else:
            with open(create.keys_file(), "wb") as keys:
                keys.write(previous)
        raise commands.Stop(f"that key cannot decrypt {config_repository}; check the password manager entry; the keys file is unchanged")


def join(argv):
    name = (argv[0] if argv else "") or os.environ.get("NAME", "")
    if not name:
        raise commands.Stop("usage: make join-installation NAME=<installation name>")
    installation.require_valid_name(name)
    os.environ.setdefault("SOPS_AGE_KEY_FILE", os.path.join(os.environ["HOME"], ".config", "sops", "age", "keys.txt"))
    installation.move_into(name, "join-installation")
    repository = create.engine_repository()
    owner = os.environ.get("GITHUB_OWNER") or repository.split("/", 1)[0]
    config_repository = f"{owner}/media-server-config-{name}"
    config = installation.config_dir()
    commands.run([os.path.join(SCRIPTS_DIR, "bootstrap.sh")])
    deploy_keys.deploy(repository, f"{config_repository}:write")
    commands.run(["git", "-C", installation.engine_dir(), "remote", "set-url", "origin", f"github-{repository.rsplit('/', 1)[-1]}:{repository}.git"])
    if not os.path.isdir(os.path.join(config, ".git")):
        commands.run(["git", "clone", f"github-media-server-config-{name}:{config_repository}.git", config])
    installation.load_installation()
    ensure_secrets_key(name, config_repository)
    restore = {} if os.environ.get("SKIP_RESTORE") else {"RESTORE_FROM_BACKUP": "1"}
    commands.run([installation.engine_run(), "setup-machine"], env=dict(os.environ, **restore) if restore else None)


def main(argv):
    return program.run("join-installation", join, argv)
