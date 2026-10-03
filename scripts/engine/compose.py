import os

import yaml

from engine import commands, installation


def optional_services_pinned():
    try:
        with open(os.path.join(installation.config_dir(), "images.yml")) as images:
            services = (yaml.safe_load(images) or {}).get("services") or {}
    except (OSError, yaml.YAMLError, AttributeError):
        return ""
    return ",".join(name for name in ("homepage",) if (services.get(name) or {}).get("image"))


def profiles(requested):
    return ",".join(profile for profile in (requested, optional_services_pinned()) if profile)


def stack(args, wiring):
    engine, config = installation.engine_dir(), installation.config_dir()
    files = ["-f", os.path.join(engine, "docker-compose.yml"), "-f", os.path.join(config, "images.yml")]
    override = os.path.join(config, "compose.override.yml")
    if os.path.isfile(override):
        files += ["-f", override]
    argv = [
        "docker", "compose", "--project-name", "media-server", "--project-directory", engine,
        "--env-file", os.path.join(engine, ".env"), *files, *args,
    ]
    requested = "wiring" if wiring else os.environ.get("COMPOSE_PROFILES", "")
    return argv, dict(os.environ, COMPOSE_PROFILES=profiles(requested))


def run(*args, wiring=False, check=True):
    argv, env = stack(args, wiring)
    return commands.run(argv, env=env, check=check)


def output(*args, wiring=False):
    argv, env = stack(args, wiring)
    return commands.output(argv, env=env)


def combined(*args, wiring=False):
    argv, env = stack(args, wiring)
    return commands.combined(argv, env=env)
