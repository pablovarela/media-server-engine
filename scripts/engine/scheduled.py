import os

from engine import commands, healthchecks, installation, program

SCRIPTS_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def scheduled_update(argv):
    installation.load_installation()
    os.environ["MACHINE_ROLE"] = installation.machine_role()
    try:
        healthchecks.ping("update", "/start")
        commands.run([os.path.join(SCRIPTS_DIR, "bootstrap.sh"), "--pinned-tools"])
        commands.run([os.path.join(SCRIPTS_DIR, "check-tools.sh")])
        commands.run([os.path.join(SCRIPTS_DIR, "engine-run"), "update"])
    except BaseException:
        with program.finishing():
            healthchecks.ping("update", "/fail")
        raise
    healthchecks.ping("update")


def main(argv):
    return program.run("scheduled-update", scheduled_update, argv)
