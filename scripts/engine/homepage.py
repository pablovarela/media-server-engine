import contextlib
import hashlib
import http.client
import json
import os
import re
import shutil
import sys
import urllib.request

import yaml

from engine import commands, healthchecks, installation, program


def read(path):
    try:
        with open(path) as source:
            return source.read()
    except FileNotFoundError:
        return ""


def dotenv(path):
    values = {}
    for line in read(path).splitlines():
        key, _, value = line.partition("=")
        if key:
            values[key] = value
    return values


def healthchecks_api_key():
    return dotenv(os.path.join(installation.engine_dir(), ".secrets", "healthchecks.env")).get("HEALTHCHECKS_API_KEY", "")


def placeholders(text):
    for name, value in (
        ("INSTALLATION_NAME", os.environ["INSTALLATION_NAME"]),
        ("ENGINE_VERSION", os.environ.get("HOMEPAGE_ENGINE_VERSION", "")),
        ("HOST", os.environ["HOMEPAGE_HOST"]),
        ("ENGINE_URL", os.environ.get("HOMEPAGE_ENGINE_URL", "")),
    ):
        text = text.replace(f"@{name}@", value)
    return text


def page_file(name):
    declared = os.path.join(installation.config_dir(), "homepage", name)
    source = declared if os.path.exists(declared) else os.path.join(installation.engine_dir(), "homepage", name)
    return placeholders(read(source))


def without_backup_status(entries):
    kept = []
    for entry in entries:
        name, value = next(iter(entry.items()))
        if name == "Healthchecks":
            continue
        kept.append({name: without_backup_status(value) if isinstance(value, list) else value})
    return kept


HEALTHCHECK_MARKER = re.compile(r"@HEALTHCHECK_([A-Z]+)@")


def existing_checks():
    url = os.environ.get("HEALTHCHECKS_API_URL", "https://healthchecks.io/api/v3/checks/")
    request = urllib.request.Request(url, headers={"X-Api-Key": healthchecks_api_key()})
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            checks = json.load(response)["checks"]
    except (OSError, ValueError, KeyError) as error:
        print(f"could not read the checks from healthchecks ({error}); the page is drawn without them", file=sys.stderr)
        return set()
    return {check.get("slug") for check in checks}


def check_slug(job):
    return os.environ.get(f"HOMEPAGE_HEALTHCHECK_{job}", f"{os.environ['INSTALLATION_NAME']}-{job.lower()}")


def with_check_slugs(tile, existing):
    text = json.dumps(tile)
    jobs = set(HEALTHCHECK_MARKER.findall(text))
    if any(check_slug(job) not in existing for job in jobs):
        return None
    for job in jobs:
        text = text.replace(f"@HEALTHCHECK_{job}@", check_slug(job))
    return json.loads(text)


def with_health_checks(entries, existing):
    kept = []
    for entry in entries:
        name, value = next(iter(entry.items()))
        if isinstance(value, list):
            found = with_health_checks(value, existing)
            if value and not found:
                continue
            value = found
        elif isinstance(value, dict):
            value = with_check_slugs(value, existing)
            if value is None:
                continue
        kept.append({name: value})
    return kept


def rendered_services(text):
    groups = yaml.safe_load(text) or []
    if not healthchecks_api_key():
        groups = without_backup_status(groups)
    elif HEALTHCHECK_MARKER.search(text):
        groups = with_health_checks(groups, existing_checks())
    return yaml.safe_dump(groups, sort_keys=False, allow_unicode=True)


def rendered_widgets(text):
    widgets = yaml.safe_load(text) or []
    for widget in widgets:
        for options in widget.values():
            if isinstance(options, dict) and options.get("href") == "":
                options.pop("href")
                options.pop("target", None)
    return yaml.safe_dump(widgets, sort_keys=False)


def as_written(text):
    return text


RENDERERS = {
    "settings.yaml": as_written,
    "services.yaml": rendered_services,
    "widgets.yaml": rendered_widgets,
    "bookmarks.yaml": as_written,
    "custom.css": as_written,
}


def copy_images(out):
    images = out.rstrip("/") + "-images"
    os.makedirs(images, exist_ok=True)
    for name in os.listdir(images):
        path = os.path.join(images, name)
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path)
        else:
            os.remove(path)
    declared = os.path.join(installation.config_dir(), "homepage", "images")
    if os.path.isdir(declared):
        shutil.copytree(declared, images, dirs_exist_ok=True)


def render(out):
    os.makedirs(out, exist_ok=True)
    for name, rendered in RENDERERS.items():
        with open(os.path.join(out, name), "w") as target:
            target.write(rendered(page_file(name)))
    copy_images(out)


def seerr_key():
    try:
        return json.loads(read(os.path.join(installation.data_dir(), "volumes", "seerr", "config", "settings.json")))["main"]["apiKey"]
    except (ValueError, KeyError):
        return ""


def bazarr_key():
    config = yaml.safe_load(read(os.path.join(installation.data_dir(), "volumes", "bazarr", "config", "config", "config.yaml"))) or {}
    return (config.get("auth") or {}).get("apikey", "")


def wiring_state(name):
    return read(os.path.join(installation.data_dir(), "volumes", ".wiring", name)).strip()


def env_text():
    apps = dotenv(os.path.join(installation.engine_dir(), ".secrets", "apps.env"))
    return "".join(
        f"HOMEPAGE_VAR_{name}={value}\n"
        for name, value in (
            ("SONARR_KEY", apps.get("SONARR_API_KEY", "")),
            ("RADARR_KEY", apps.get("RADARR_API_KEY", "")),
            ("PROWLARR_KEY", apps.get("PROWLARR_API_KEY", "")),
            ("DELUGE_PASSWORD", apps.get("DELUGE_WEB_PASSWORD", "")),
            ("JELLYFIN_KEY", wiring_state("jellyfin.key")),
            ("SEERR_KEY", seerr_key()),
            ("BAZARR_KEY", bazarr_key()),
            ("GLUETUN_KEY", wiring_state("gluetun-control.key")),
            ("HEALTHCHECKS_KEY", healthchecks_api_key()),
        )
    )


def env():
    print(env_text(), end="")


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


def running():
    state = commands.output(["docker", "inspect", "-f", "{{.State.Running}}", "homepage"], check=False, discard_errors=True)
    return state.strip() == "true"


def redraw(role):
    before = images_signature()
    with environment(
        HOMEPAGE_HOST=installation.network_name(),
        HOMEPAGE_ENGINE_VERSION=installation.engine_version(),
        HOMEPAGE_ENGINE_URL=installation.engine_page_url(),
        HOMEPAGE_HEALTHCHECK_BACKUP=healthchecks.slug("backup", role),
        HOMEPAGE_HEALTHCHECK_VERIFY=healthchecks.slug("verify", role),
        HOMEPAGE_HEALTHCHECK_UPDATE=healthchecks.slug("update", role),
    ):
        render(os.path.join(installation.engine_dir(), ".homepage"))
    if not running():
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


def redraw_installation(argv):
    installation.load_installation()
    redraw(installation.machine_role())
    print("The landing page is redrawn; an open page reloads itself in a few seconds.")


def main(argv):
    return program.run("homepage", redraw_installation, argv)
