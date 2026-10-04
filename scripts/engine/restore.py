import os
import time

from engine import commands, installation, program, restic

RUNNING_STACK = ["docker", "ps", "-q", "--filter", "label=com.docker.compose.project=media-server"]


def has_app_data(volumes):
    return os.path.isdir(volumes) and any(name != "configarr" for name in os.listdir(volumes))


def move_existing_volumes_aside(data):
    aside = time.strftime("volumes.before-restore-%Y%m%d-%H%M%S")
    os.rename(os.path.join(data, "volumes"), os.path.join(data, aside))
    os.mkdir(os.path.join(data, "volumes"))
    print(f"previous volumes/ kept in {data}/{aside}; delete it once the restore looks right")


def restore(argv):
    installation.load_installation()
    data = installation.data_dir()
    os.makedirs(data, exist_ok=True)
    os.chdir(data)
    try:
        running = commands.output(RUNNING_STACK)
    except commands.CommandFailed:
        raise commands.Stop("cannot tell whether the stack is running (docker ps failed)") from None
    if running.strip():
        raise commands.Stop("the stack is running; stop it with make media-stop first")
    if has_app_data(os.path.join(data, "volumes")):
        if argv[:1] != ["--overwrite"]:
            raise commands.Stop("volumes/ already holds app data; run with --overwrite to replace it with the latest backup")
        move_existing_volumes_aside(data)
    restic.run("unlock")
    restic.run_explaining_locks("restore", "latest:/volumes", *restic.snapshot_filter(), "--target", os.path.join(data, "volumes"), "--exclude", "configarr")


def main(argv):
    return program.run("restore", restore, argv)
