import sys

from engine import apps, backups, claim, images, restore, role, unlock, update, verify, version

COMMANDS = {
    "update": update.main,
    "backup": backups.main,
    "verify-backup": verify.main,
    "backup-role": role.main,
    "claim-backup-main": claim.main,
    "unlock-backup": unlock.main,
    "restore": restore.main,
    "prune-stack-images": images.main,
    "urls": apps.urls_main,
    "logins": apps.logins_main,
    "version": version.main,
}


def main(argv):
    if not argv or argv[0] not in COMMANDS:
        print("usage: engine-run <command> [arguments]", file=sys.stderr)
        print("commands: " + ", ".join(sorted(COMMANDS)), file=sys.stderr)
        return 1
    return COMMANDS[argv[0]](argv[1:])
