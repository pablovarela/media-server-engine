import os

import yaml

from engine import commands, installation, program


def optional_services_pinned():
    try:
        with open(os.path.join(installation.config_dir(), "images.yml")) as images:
            services = (yaml.safe_load(images) or {}).get("services") or {}
        return ",".join(name for name in ("homepage",) if (services.get(name) or {}).get("image"))
    except (OSError, yaml.YAMLError, AttributeError):
        return ""


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


def output(*args, wiring=False, check=True):
    argv, env = stack(args, wiring)
    return commands.output(argv, env=env, check=check)


def combined(*args, wiring=False):
    argv, env = stack(args, wiring)
    return commands.combined(argv, env=env)


def monitoring(args):
    engine, config = installation.engine_dir(), installation.config_dir()
    return [
        "docker", "compose", "--project-name", "monitoring", "--project-directory", engine, "--env-file", os.path.join(engine, ".env"),
        "-f", os.path.join(engine, "docker-compose.monitoring.yml"), "-f", os.path.join(config, "images.monitoring.yml"), *args,
    ]


def monitoring_output(*args):
    return commands.output(monitoring(args))


def run_monitoring(*args, check=True):
    return commands.run(monitoring(args), check=check)


def stack_command(argv):
    return run(*argv, check=False)


def monitoring_command(argv):
    return run_monitoring(*argv, check=False)


def stack_main(argv):
    return program.run("stack", stack_command, argv)


def monitoring_main(argv):
    return program.run("monitoring", monitoring_command, argv)
