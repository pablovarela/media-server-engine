import json
import os
import sys

from engine import commands, compose, installation


def problems_in(config):
    volumes = os.path.realpath(installation.data_dir()) + "/volumes/"
    problems = []
    for name, service in config["services"].items():
        image = service.get("image", "(none)")
        if "@sha256:" not in image:
            problems.append(f"{name}: image {image} is not pinned to a digest")
        writes_state = any(
            mount.get("type") == "bind" and os.path.realpath(mount.get("source", "")).startswith(volumes)
            for mount in service.get("volumes", [])
        )
        environment = service.get("environment") or {}
        runs_as_1000 = service.get("user") == "1000:1000" or (environment.get("PUID") == "1000" and environment.get("PGID") == "1000")
        if writes_state and not runs_as_1000:
            problems.append(f"{name}: writes app state but does not run as uid and gid 1000")
    return problems


def merged(config):
    try:
        return json.loads(config())
    except commands.CommandFailed:
        raise commands.Stop("the compose files cannot be merged; see the error above") from None


def check():
    os.makedirs(os.path.join(installation.data_dir(), "volumes"), exist_ok=True)
    stack = merged(lambda: compose.output("config", "--format", "json", wiring=True))
    monitoring = merged(lambda: compose.monitoring_output("config", "--format", "json"))
    problems = problems_in(stack) + problems_in(monitoring)
    if problems:
        print("\n".join(problems), file=sys.stderr)
        raise commands.Stop("the merged compose files are not safe to deploy")
