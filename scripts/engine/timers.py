import grp
import os
import pwd
import shutil

from engine import commands, installation, program, role

BACKUP_TIMERS = ("media-backup", "media-verify")


def absolute(path):
    os.makedirs(path, exist_ok=True)
    return os.path.abspath(path)


def render(text):
    replacements = {
        "@ENGINE_DIR@": absolute(installation.engine_dir()),
        "@CONFIG_DIR@": absolute(installation.config_dir()),
        "@DATA_DIR@": absolute(installation.data_dir()),
        "@USER@": pwd.getpwuid(os.geteuid()).pw_name,
        "@GROUP@": grp.getgrgid(os.getegid()).gr_name,
        "@HOME@": os.environ["HOME"],
        "@SOPS@": shutil.which("sops") or "",
    }
    for placeholder, value in replacements.items():
        text = text.replace(placeholder, value)
    return text


def install(names):
    if not names:
        raise commands.Stop("usage: install-timers UNIT_NAME..., for example media-backup")
    if not installation.systemd_running():
        raise commands.Stop("timers need systemd, and systemd is not running on this machine")
    backup = any(name in BACKUP_TIMERS for name in names)
    if backup:
        installation.load_installation()
        if role.is_main() != 0:
            raise commands.Stop(f"this machine is not {os.environ['INSTALLATION_NAME']}'s main; run make claim-backup-main first")
    unit_dir = os.environ.get("UNIT_DIR") or "/etc/systemd/system"
    for name in names:
        for suffix in (".service", ".timer"):
            with open(os.path.join(installation.engine_dir(), "systemd", name + suffix)) as unit:
                text = render(unit.read())
            commands.run(["sudo", "tee", os.path.join(unit_dir, name + suffix)], input=text, discard_output=True)
    if backup:
        role.mark_main()
    commands.run(["sudo", "systemctl", "daemon-reload"])
    commands.run(["sudo", "systemctl", "enable", "--now", *[f"{name}.timer" for name in names]])


def main(argv):
    return program.run("install-timers", install, argv)
