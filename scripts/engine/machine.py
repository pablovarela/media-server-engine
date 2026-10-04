import json
import os
import shlex
import sys

from engine import commands, installation, program, prompt

RULE = "=" * 64
SCRIPTS_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def secrets(name):
    return os.path.join(installation.config_dir(), "secrets", name)


def with_backup_secrets(*words, check=True, capture=False, quiet=False):
    argv = ["sops", "exec-env", secrets("backup.sops.env"), shlex.join(words)]
    return commands.output(argv, check=check, discard_errors=quiet) if capture else commands.run(argv, check=check)


def has_backups():
    listing = with_backup_secrets("restic", "snapshots", "--no-lock", "--latest", "1", "--json", check=False, capture=True, quiet=True)
    try:
        return len(json.loads(listing) or []) > 0
    except ValueError:
        return False


def wants_to_be_main():
    run = installation.engine_run()
    default = "y"
    if with_backup_secrets(run, "backup-role", "is-main", check=False) != 0:
        main = with_backup_secrets(run, "backup-role", "describe-main", check=False, capture=True).strip()
        print(f"another machine is {os.environ['INSTALLATION_NAME']}'s main ({main}); say yes only to take over from it", file=sys.stderr)
        default = "n"
    return prompt.ask(f"Make this machine {os.environ['INSTALLATION_NAME']}'s main, the one that backs up? (y/n)", default) in ("y", "Y")


def claim():
    inner = shlex.join(["sops", "exec-env", secrets("backup.sops.env"), shlex.join([installation.engine_run(), "claim-backup-main"])])
    commands.run(["sops", "exec-env", secrets("healthchecks.sops.env"), inner], env=dict(os.environ, CLAIM_CONFIRMED="1"))


def print_summary(main):
    name = os.environ["INSTALLATION_NAME"]
    place = os.path.dirname(os.path.abspath(installation.engine_dir()))
    header = ["", RULE, f"{name} is ready on {installation.network_name()}.", "", f"It lives in {place}; run make from there.", "", "Apps (make urls lists them again):"]
    print("\n".join(header), flush=True)
    urls = commands.output([installation.engine_run(), "urls"])
    lines = [f"  {line}" for line in urls.splitlines()]
    lines += [
        "Their logins: make logins (shows the passwords on this terminal).", "", "Everyday commands:",
        "  make configure    change settings and secrets, then make update applies them",
        "  make update       apply config changes and update the apps",
        "  make media-stop   stop the apps; make media-start starts them again",
    ]
    if main:
        lines.append("  make backup-now   back up now")
    lines.append("")
    if installation.systemd_running():
        lines.append("This machine updates itself daily at 05:00 and removes fake downloads every 15 minutes.")
        lines.append("It is the main: it backs up daily at 04:30 and checks the backups on Sundays at 05:30." if main else "Another machine backs up; this one never does.")
    else:
        lines.append("This machine has no systemd, so nothing runs on its own:")
        if main:
            lines.append("  run make update after config changes, and make backup-now to back up.")
        else:
            lines += ["  run make update after config changes.", "Another machine backs up; this one never does."]
    lines += [RULE, "", "Go to the installation, to run make from it:", f"  cd {place}"]
    print("\n".join(lines))


def setup(argv):
    os.environ.setdefault("SOPS_AGE_KEY_FILE", os.path.join(os.environ["HOME"], ".config", "sops", "age", "keys.txt"))
    installation.load_installation()
    commands.run([os.path.join(SCRIPTS_DIR, "check-tools.sh")])
    run = installation.engine_run()
    if os.environ.get("RESTORE_FROM_BACKUP") and has_backups():
        with_backup_secrets(run, "restore")
    main = wants_to_be_main()
    commands.run([run, "update"], env=dict(os.environ, MACHINE_ROLE="main") if main else None)
    if installation.systemd_running():
        commands.run([run, "install-timers", "media-update", "media-download-cleanup"])
        if main:
            claim()
            with_backup_secrets(run, "install-timers", "media-backup", "media-verify")
    elif main:
        claim()
    print_summary(main)


def main(argv):
    return program.run("setup-machine", setup, argv)
