import sys

from engine import apps, backups, claim, compose, configure, create, downloads, homepage, images, installation, join, machine, ports, program, restore, role, scheduled, timers, unlock, update, verify, version


def installation_required(argv):
    installation.require()


def require_installation(argv):
    return program.run("require-installation", installation_required, argv)


COMMANDS = {
    "update": update.main,
    "backup": backups.main,
    "verify-backup": verify.main,
    "backup-role": role.main,
    "claim-backup-main": claim.main,
    "configure": configure.main,
    "unlock-backup": unlock.main,
    "restore": restore.main,
    "prune-stack-images": images.main,
    "urls": apps.urls_main,
    "logins": apps.logins_main,
    "version": version.main,
    "remove-executable-downloads": downloads.main,
    "port-in-use": ports.main,
    "homepage": homepage.main,
    "scheduled-update": scheduled.main,
    "require-installation": require_installation,
    "stack": compose.stack_main,
    "monitoring": compose.monitoring_main,
    "install-timers": timers.main,
    "setup-machine": machine.main,
    "create-installation": create.main,
    "join-installation": join.main,
}


def main(argv):
    if not argv or argv[0] not in COMMANDS:
        print("usage: engine-run <command> [arguments]", file=sys.stderr)
        print("commands: " + ", ".join(sorted(COMMANDS)), file=sys.stderr)
        return 1
    return COMMANDS[argv[0]](argv[1:])
