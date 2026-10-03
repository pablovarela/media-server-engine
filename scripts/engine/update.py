import contextlib
import hashlib
import http.client
import json
import os
import platform
import pwd
import stat
import sys
import time
import urllib.request

from engine import backups, commands, compose, healthchecks, homepage, installation, secrets

SCRIPTS_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
UPDATE_PROGRAM = os.path.join(SCRIPTS_DIR, "update.py")
GLUETUN_DEPENDENTS = ["prowlarr", "flaresolverr", "deluge"]


def git(repository, *args):
    return ["git", "-C", repository, *args]


def config_has_remote():
    return commands.succeeds(git(installation.config_dir(), "remote", "get-url", "origin"))


def require_clean_engine():
    engine = installation.engine_dir()
    changes = commands.output(git(engine, "status", "--porcelain", "--untracked-files=no")).rstrip("\n")
    if changes:
        print(changes, file=sys.stderr)
        raise commands.Stop(f"uncommitted changes in the engine at {engine}; an installation's engine is not edited, change the engine repository instead")


def require_clean_config():
    config = installation.config_dir()
    changes = commands.output(git(config, "status", "--porcelain")).rstrip("\n")
    if not changes:
        return
    lines = ["The config has changes that are not committed:", changes, f"See them with: git -C {config} diff"]
    if config_has_remote():
        lines += ["Commit and push them, then run make update again:", f'  git -C {config} commit -am "<what changed>" && git -C {config} push']
    else:
        lines += ["Commit them, then run make update again:", f'  git -C {config} commit -am "<what changed>"']
    raise commands.Stop("\n".join(lines), prefixed=False)


def seconds(name, default):
    return int(os.environ.get(name) or default)


def wait_for_running_backup():
    deadline = time.monotonic() + seconds("UPDATE_BACKUP_WAIT_SECONDS", 3600)
    announced = False
    while backups.running_backup_pid():
        if time.monotonic() >= deadline:
            raise commands.Stop("a backup is still running; run make update again once it has finished")
        if not announced:
            print("Waiting for the running backup to finish...", flush=True)
            announced = True
        time.sleep(seconds("UPDATE_BACKUP_POLL_SECONDS", 10))


def pinned_engine_version():
    with open(os.path.join(installation.config_dir(), "engine.env")) as pins:
        return next((line.strip()[len("ENGINE_VERSION="):] for line in pins if line.startswith("ENGINE_VERSION=")), "")


def restart_program():
    if os.path.exists(UPDATE_PROGRAM):
        return UPDATE_PROGRAM
    return os.path.join(os.path.dirname(UPDATE_PROGRAM), "update.sh")


def switch_engine_and_restart_if_needed(argv):
    engine = installation.engine_dir()
    wanted = pinned_engine_version()
    if not wanted or wanted == "local":
        return
    current = commands.output(git(engine, "describe", "--tags", "--exact-match"), check=False, discard_errors=True).strip()
    if current == wanted:
        return
    commands.run(git(engine, "fetch", "--tags", "--quiet"))
    if not commands.succeeds(git(engine, "rev-parse", "-q", "--verify", f"refs/tags/{wanted}")):
        raise commands.Stop(f"engine version {wanted} not found; staying on {current or 'the current checkout'}")
    commands.run(git(engine, "checkout", "-q", "--detach", wanted))
    os.environ["MEDIA_SERVER_PULLED"] = "1"
    program = restart_program()
    os.execv(program, [program, *argv])


def docker_socket_gid():
    socket_path = "/var/run/docker.sock"
    if platform.system() == "Linux" and os.path.exists(socket_path) and stat.S_ISSOCK(os.stat(socket_path).st_mode):
        return os.stat(socket_path).st_gid
    return 0


def homepage_allowed_hosts():
    port = installation.homepage_port()
    suffix = "" if port == "80" else f":{port}"
    extra = os.environ.get("HOMEPAGE_ALLOWED_HOSTS", "").replace(",", " ").split()
    return ",".join(host + suffix for host in [installation.network_name(), "localhost", "127.0.0.1", *extra])


def write_compose_env():
    with open(os.path.join(installation.engine_dir(), ".env"), "w") as env:
        env.write(
            f"DOCKER_GID={docker_socket_gid()}\nTZ={os.environ.get('TZ') or 'Etc/UTC'}\n"
            f"HOMEPAGE_PORT={installation.homepage_port()}\nHOMEPAGE_ALLOWED_HOSTS={homepage_allowed_hosts()}\n"
        )


def machine_role():
    return os.environ.get("MACHINE_ROLE") or installation.machine_role()


def check_slug(job, role):
    name = os.environ["INSTALLATION_NAME"]
    if job == "update" and role == "secondary":
        return f"{name}-{job}-{installation.short_hostname()}"
    return f"{name}-{job}"


@contextlib.contextmanager
def environment(**values):
    saved = {name: os.environ.get(name) for name in values}
    os.environ.update(values)
    try:
        yield
    finally:
        for name, value in saved.items():
            if value is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = value


def images_signature():
    images = os.path.join(installation.engine_dir(), ".homepage-images")
    signature = []
    for directory, _, files in os.walk(images):
        for name in files:
            path = os.path.join(directory, name)
            with open(path, "rb") as image:
                signature.append((os.path.relpath(path, images), hashlib.sha256(image.read()).hexdigest()))
    return sorted(signature)


def homepage_running():
    running = commands.output(["docker", "inspect", "-f", "{{.State.Running}}", "homepage"], check=False, discard_errors=True)
    return running.strip() == "true"


def render_landing_page():
    before = images_signature()
    role = machine_role()
    with environment(
        HOMEPAGE_HOST=installation.network_name(),
        HOMEPAGE_ENGINE_VERSION=installation.engine_version(),
        HOMEPAGE_ENGINE_URL=installation.engine_page_url(),
        HOMEPAGE_HEALTHCHECK_BACKUP=check_slug("backup", role),
        HOMEPAGE_HEALTHCHECK_VERIFY=check_slug("verify", role),
        HOMEPAGE_HEALTHCHECK_UPDATE=check_slug("update", role),
    ):
        homepage.render(os.path.join(installation.engine_dir(), ".homepage"))
    if not homepage_running():
        return
    if images_signature() != before:
        commands.run(["docker", "restart", "homepage"], discard_output=True)
        return
    try:
        revalidate = urllib.request.Request(f"http://localhost:{installation.homepage_port()}/api/revalidate")
        with urllib.request.urlopen(revalidate, timeout=10):
            pass
    except (OSError, http.client.HTTPException):
        pass


def homepage_env_changed():
    path = os.path.join(installation.engine_dir(), ".secrets", "homepage.env")
    text = homepage.env_text()
    try:
        with open(path) as current:
            if current.read() == text:
                return False
    except FileNotFoundError:
        pass
    secrets.write_private(path, text)
    return True


def refresh_homepage():
    if homepage_env_changed() and "homepage" in compose.optional_services_pinned().split(","):
        compose.run("up", "-d", "homepage")


def checks_this_machine_sets_up():
    if machine_role() == "main":
        return [(job, check_slug(job, "main")) for job in ("backup", "verify", "update")]
    return [("update", check_slug("update", "secondary"))]


def installation_directory_as_typed():
    root = installation.installation_root() or os.path.dirname(installation.engine_dir())
    home = os.path.realpath(os.environ["HOME"])
    if root.startswith(home + os.sep):
        return "~/" + root[len(home) + 1:]
    return root


def set_up_healthchecks():
    if not installation.systemd_running():
        return
    try:
        facts = healthchecks.Facts(
            name=os.environ["INSTALLATION_NAME"],
            tz=healthchecks.timers_time_zone(),
            repository=os.environ.get("RESTIC_REPOSITORY", ""),
            ssh=f"{pwd.getpwuid(os.geteuid()).pw_name}@{installation.network_name()}",
            directory=installation_directory_as_typed(),
        )
        healthchecks.sync(checks_this_machine_sets_up(), facts)
    except Exception as error:
        print(f"could not set up the healthchecks.io checks ({error}); carrying on", file=sys.stderr)


def create_bind_mount_directories():
    data = installation.data_dir()
    for service in json.loads(compose.output("config", "--format", "json", wiring=True))["services"].values():
        for mount in service.get("volumes", []):
            if mount.get("type") == "bind" and mount.get("source", "").startswith(data):
                os.makedirs(mount["source"], exist_ok=True)


def pull_images():
    attempts, attempt = seconds("UPDATE_PULL_ATTEMPTS", 4), 1
    while True:
        returncode, output = compose.combined("pull", "--quiet", wiring=True)
        output = output.rstrip("\n")
        if returncode == 0:
            break
        print(output, file=sys.stderr)
        if "toomanyrequests" not in output and "Too Many Requests" not in output:
            raise commands.CommandFailed(1)
        if attempt >= attempts:
            raise commands.Stop("a registry kept refusing pulls as too many requests; run make update again later")
        wait = seconds("UPDATE_PULL_RETRY_SECONDS", 30) * attempt
        print(f"A registry is limiting requests; trying the pull again in {wait} seconds...", flush=True)
        time.sleep(wait)
        attempt += 1
    if output:
        print(output)


def detached_gluetun_dependents():
    gluetun = compose.output("ps", "-q", "gluetun", check=False).strip()
    return [
        dependent
        for dependent in GLUETUN_DEPENDENTS
        if commands.output(["docker", "inspect", "-f", "{{.HostConfig.NetworkMode}}", dependent], check=False, discard_errors=True).strip()
        != f"container:{gluetun}"
    ]


def reattach_gluetun_dependents():
    detached = detached_gluetun_dependents()
    if detached:
        compose.run("up", "-d", "--force-recreate", "--no-deps", *detached)


def update(argv):
    require_clean_engine()
    require_clean_config()
    wait_for_running_backup()
    if not os.environ.get("MEDIA_SERVER_PULLED"):
        if config_has_remote():
            commands.run(git(installation.config_dir(), "pull", "--ff-only"))
        switch_engine_and_restart_if_needed(argv)
    commands.run([os.path.join(SCRIPTS_DIR, "bootstrap.sh"), "--pinned-tools"])
    installation.load_installation()
    installation.write_installation_makefile()
    secrets.decrypt_all()
    write_compose_env()
    render_landing_page()
    homepage_env_changed()
    set_up_healthchecks()
    commands.run([os.path.join(SCRIPTS_DIR, "check-stack.sh")])
    create_bind_mount_directories()
    pull_images()
    up = compose.run("up", "-d", "--remove-orphans", check=False)
    reattach_gluetun_dependents()
    if up:
        raise commands.CommandFailed(up)
    wired = commands.run([os.path.join(SCRIPTS_DIR, "wire", "wire_apps.py")], check=False)
    refresh_homepage()
    if wired:
        raise commands.CommandFailed(wired)
    commands.run([os.path.join(SCRIPTS_DIR, "prune-stack-images.sh")])


def main(argv):
    try:
        update(argv)
    except commands.Stop as stop:
        print(stop.text("update"), file=sys.stderr)
        return 1
    except commands.CommandFailed as failed:
        return failed.returncode
    except (OSError, ValueError) as error:
        print(f"update: {error}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        return 130
    return 0
