import filecmp
import glob
import os
import platform
import re
import shutil
import socket

from engine import commands

SHELL_OWN = {"PWD", "OLDPWD", "SHLVL", "_"}
CHECKOUT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def export_directories():
    engine = os.environ.get("ENGINE_DIR") or CHECKOUT
    os.environ["ENGINE_DIR"] = engine
    os.environ["CONFIG_DIR"] = os.environ.get("CONFIG_DIR") or os.path.join(os.path.dirname(engine), "config")
    os.environ["DATA_DIR"] = os.environ.get("DATA_DIR") or os.path.join(os.path.dirname(engine), "data")


def engine_dir():
    return os.environ["ENGINE_DIR"]


def config_dir():
    return os.environ["CONFIG_DIR"]


def data_dir():
    return os.environ["DATA_DIR"]


def installations_on_this_machine():
    for config in sorted(glob.glob(os.path.join(os.environ["HOME"], "*", "config", "installation.env"))):
        root = os.path.dirname(os.path.dirname(config))
        if os.path.isdir(os.path.join(root, "engine")):
            yield root


def not_an_installation():
    lines = [f"{engine_dir()} is not an installation: there is no config next to it."]
    found = list(installations_on_this_machine())
    if found:
        lines.append("Installations on this machine; run make from the one you mean:")
        lines.extend(f"  cd {root}" for root in found)
    lines.append("To create one: make create-installation NAME=<name>")
    return commands.Stop("\n".join(lines), prefixed=False)


def sourced(path):
    listing = commands.output(["bash", "-c", 'set -a; source "$1" >/dev/null || exit; env -0', "installation", path], env=dict(os.environ))
    values = dict(entry.partition("=")[::2] for entry in listing.split("\0") if entry)
    return {name: value for name, value in values.items() if name not in SHELL_OWN and os.environ.get(name) != value}


def load_installation():
    path = os.path.join(config_dir(), "installation.env")
    if not os.path.isfile(path):
        raise not_an_installation()
    os.environ.update(sourced(path))
    if not os.environ.get("INSTALLATION_NAME"):
        raise commands.Stop(f"INSTALLATION_NAME is not set in {path}")


def installation_root():
    root = os.path.realpath(os.path.join(engine_dir(), ".."))
    if os.path.basename(engine_dir()) == "engine" and os.path.realpath(config_dir()) == os.path.join(root, "config"):
        return root
    return None


def write_installation_makefile():
    root = installation_root()
    if root is None:
        return
    source, target = os.path.join(engine_dir(), "installation", "Makefile"), os.path.join(root, "Makefile")
    if not (os.path.exists(target) and filecmp.cmp(source, target, shallow=False)):
        shutil.copy(source, target)


def machine_role():
    return "main" if os.path.exists(os.path.join(data_dir(), ".backup-main")) else "secondary"


def short_hostname():
    return socket.gethostname().split(".")[0]


def network_name():
    if os.environ.get("MEDIA_SERVER_HOST"):
        return os.environ["MEDIA_SERVER_HOST"]
    if platform.system() == "Darwin":
        return commands.output(["scutil", "--get", "LocalHostName"]).strip() + ".local"
    return short_hostname() + ".local"


def systemd_running():
    return os.path.isdir(os.environ.get("SYSTEMD_RUNTIME_DIR", "/run/systemd/system")) and shutil.which("systemctl") is not None


def homepage_port():
    return os.environ.get("HOMEPAGE_PORT") or "80"


def engine_git(*args):
    return commands.output(["git", "-C", engine_dir(), *args], check=False, discard_errors=True).strip()


def engine_version():
    return engine_git("describe", "--tags", "--always")


def engine_page_url():
    remote = engine_git("remote", "get-url", "origin")
    if "github" not in remote:
        return ""
    found = re.match(r"^.*[:/]([^/:]+/[^/:]+)$", re.sub(r"\.git$", "", remote))
    if not found:
        return ""
    version = engine_version()
    if re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", version):
        return f"https://github.com/{found.group(1)}/releases/tag/{version}"
    return f"https://github.com/{found.group(1)}/commit/{engine_git('rev-parse', 'HEAD')}"
