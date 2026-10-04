import os
import sys

from engine import backups, commands, installation, program, prompt, restic, role


def create_repository_if_missing():
    if commands.quiet(["restic", "cat", "config"]) == restic.NO_REPOSITORY:
        print(f"Creating the backup repository {os.environ.get('RESTIC_REPOSITORY', '')}")
        restic.run("init")


def confirm_taking_over():
    status = role.is_main()
    if status == 0:
        return
    if status != role.NOT_THE_MAIN:
        raise commands.Stop(f"{role.explain(status)}; nothing was claimed")
    if os.environ.get("CLAIM_CONFIRMED"):
        return
    print(f"{os.environ['INSTALLATION_NAME']}'s main is {role.describe_main()}. Taking over makes it refuse to back up.", file=sys.stderr)
    if prompt.ask("Make this machine the main instead? (y/n)", "n") not in ("y", "Y"):
        raise commands.Stop("nothing was claimed")


def claim(argv):
    installation.load_installation()
    create_repository_if_missing()
    confirm_taking_over()
    backups.backup(claim=True)
    role.mark_main()
    print(f"This machine is now {os.environ['INSTALLATION_NAME']}'s main; backups from any other machine are refused.")


def main(argv):
    return program.run("claim-backup-main", claim, argv)
