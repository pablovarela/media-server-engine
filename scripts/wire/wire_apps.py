#!/usr/bin/env python3
import http.client
import importlib
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

from wirelib import WiringError, without_secrets

HERE = os.path.dirname(os.path.abspath(__file__))
HEALTH = {
    "sonarr": ("SONARR_URL", "http://localhost:8989", "/ping"),
    "radarr": ("RADARR_URL", "http://localhost:7878", "/ping"),
    "prowlarr": ("PROWLARR_URL", "http://localhost:9696", "/ping"),
    "jellyfin": ("JELLYFIN_URL", "http://localhost:8096", "/System/Info/Public"),
    "deluge": ("DELUGE_URL", "http://localhost:8112", "/"),
    "seerr": ("SEERR_URL", "http://localhost:5055", "/api/v1/status"),
    "bazarr": ("BAZARR_URL", "http://localhost:6767", "/api/system/ping"),
    "maintainerr": ("MAINTAINERR_URL", "http://localhost:6246", "/api/settings/version"),
}


def wait_seconds():
    return float(os.environ.get("WIRE_WAIT_SECONDS", "300"))


def health_url(app):
    variable, default, path = HEALTH[app]
    return os.environ.get(variable, default).rstrip("/") + path


def answers(url):
    try:
        with urllib.request.urlopen(urllib.request.Request(url), timeout=5):
            return True
    except (urllib.error.URLError, http.client.HTTPException, OSError, ValueError):
        return False


def wait_until_answering(apps):
    deadline = time.monotonic() + wait_seconds()
    for app in apps:
        while not answers(health_url(app)):
            if time.monotonic() >= deadline:
                return False
            time.sleep(float(os.environ.get("WIRE_RETRY_SECONDS", "2")))
    return True


def python_step(module, function="wire"):
    def step():
        getattr(importlib.import_module(module), function)()

    return step


def configarr():
    subprocess.run([os.path.join(HERE, "configarr.sh")], check=True)


STEPS = [
    ("prowlarr", ["prowlarr"], python_step("prowlarr")),
    ("jellyfin", ["jellyfin"], python_step("jellyfin")),
    ("library-updates", ["sonarr", "radarr"], python_step("library_updates")),
    ("deluge", ["deluge"], python_step("deluge")),
    ("configarr", [], configarr),
    ("seerr", ["seerr"], python_step("seerr")),
    ("bazarr", ["bazarr"], python_step("bazarr")),
    ("maintainerr", ["maintainerr"], python_step("maintainerr")),
    ("prowlarr-sync", ["prowlarr", "sonarr", "radarr"], python_step("prowlarr", "sync_again")),
]


def run_steps(steps):
    failed = []
    for name, apps, step in steps:
        if not wait_until_answering(apps):
            print(f"{name}: {' and '.join(apps)} not answering after {wait_seconds():.0f}s; skipped its wiring", file=sys.stderr, flush=True)
            failed.append(name)
            continue
        try:
            step()
        except WiringError as error:
            print(without_secrets(f"{name}: {error}"), file=sys.stderr, flush=True)
            failed.append(name)
        except subprocess.CalledProcessError:
            failed.append(name)
        except Exception as error:
            print(without_secrets(f"{name}: stopped by {type(error).__name__}: {error}"), file=sys.stderr, flush=True)
            failed.append(name)
    if failed:
        print("wiring failed: " + " ".join(failed), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(run_steps(STEPS))
