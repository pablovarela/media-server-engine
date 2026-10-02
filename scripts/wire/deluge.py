import glob
import hashlib
import json
import os
import subprocess
import time

from wirelib import DATA_DIR, DRY_RUN, Api, WiringError, app_secrets, declared, report, run

APP = "deluge"
CONTAINER = os.environ.get("DELUGE_CONTAINER", "deluge")
CONFIG_DIR = os.path.join(DATA_DIR, "volumes", "deluge", "config")
WEB_CONF_HEADER = {"file": 2, "format": 1}
PLUGIN_CONF_HEADER = {"file": 1, "format": 1}


class DelugeConfig:
    def __init__(self, name, default_header):
        self.path = os.path.join(CONFIG_DIR, name)
        self.header, self.body = dict(default_header), {}
        if os.path.exists(self.path):
            with open(self.path) as conf:
                text = conf.read()
            decoder = json.JSONDecoder()
            self.header, end = decoder.raw_decode(text)
            self.body, _ = decoder.raw_decode(text[end:])
        self.dirty = False

    def set(self, key, value):
        self.body[key] = value
        self.dirty = True

    def save(self):
        descriptor = os.open(self.path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(descriptor, "w") as conf:
            conf.write(json.dumps(self.header, indent=4) + json.dumps(self.body, indent=4, sort_keys=True))


def password_hash(salt, password):
    digest = hashlib.sha1(salt.encode())
    digest.update(password.encode())
    return digest.hexdigest()


def docker(*args):
    try:
        return subprocess.run(["docker", *args], check=True, capture_output=True, text=True).stdout.strip()
    except subprocess.CalledProcessError as error:
        raise WiringError(f"docker {args[0]} {CONTAINER} failed: {(error.stderr or '').strip()[-300:]}") from None


def container_python():
    return docker("exec", CONTAINER, "python3", "-c", 'import sys; print("%d.%d" % sys.version_info[:2])')


def build_plugin(plugin):
    script = (
        'set -e; d=$(mktemp -d); trap "rm -rf $d" EXIT; cd "$d"; '
        f'curl -fsSL -o src.tgz "{plugin["source"]}"; '
        f'echo "{plugin["sha256"]}  src.tgz" | sha256sum -c -; '
        'tar xzf src.tgz; cd */; python3 setup.py -q bdist_egg; cp dist/*.egg /config/plugins/'
    )
    docker("exec", "-u", "abc", CONTAINER, "sh", "-c", script)


class Deluge:
    def __init__(self, password):
        self.password = password
        self.failures = []
        self.core = DelugeConfig("core.conf", PLUGIN_CONF_HEADER)
        self.web = DelugeConfig("web.conf", WEB_CONF_HEADER)
        self.plugin_configs = []

    def ensure_plugin_eggs(self, plugins):
        with_source = [p for p in plugins if p.get("source")]
        if not with_source:
            return
        python = container_python()
        for plugin in with_source:
            if glob.glob(os.path.join(CONFIG_DIR, "plugins", f"{plugin['name']}-*-py{python}.egg")):
                continue
            report(APP, f"build plugin {plugin['name']} for python {python}")
            if DRY_RUN:
                continue
            try:
                build_plugin(plugin)
            except WiringError as error:
                self.failures.append(f"could not build plugin {plugin['name']}: {error}")

    def wire_core(self, settings, plugins):
        for key, value in settings.items():
            if self.core.body.get(key) != value:
                report(APP, f"set {key} {self.core.body.get(key)} -> {value}")
                self.core.set(key, value)
        enabled = list(self.core.body.get("enabled_plugins", []))
        for plugin in plugins:
            if plugin["name"] not in enabled:
                report(APP, f"enable plugin {plugin['name']}")
                enabled.append(plugin["name"])
                self.core.set("enabled_plugins", enabled)

    def wire_plugin_settings(self, plugins):
        for plugin in plugins:
            settings = plugin.get("settings") or {}
            if not settings:
                continue
            conf = DelugeConfig(f"{plugin['name'].lower()}.conf", PLUGIN_CONF_HEADER)
            for key, value in settings.items():
                if conf.body.get(key) != value:
                    report(APP, f"set {plugin['name']} {key} {conf.body.get(key)} -> {value}")
                    conf.set(key, value)
            self.plugin_configs.append(conf)

    def wire_web_password(self):
        body = self.web.body
        if body.get("pwd_salt") and password_hash(body["pwd_salt"], self.password) == body.get("pwd_sha1"):
            if body.get("first_login") is not False:
                report(APP, "turn off the first login prompt")
                self.web.set("first_login", False)
            return
        report(APP, "set web password")
        salt = hashlib.sha1(os.urandom(32)).hexdigest()
        self.web.set("pwd_salt", salt)
        self.web.set("pwd_sha1", password_hash(salt, self.password))
        self.web.set("first_login", False)

    def changed_configs(self):
        return [conf for conf in [self.core, self.web, *self.plugin_configs] if conf.dirty]

    def apply(self):
        changed = self.changed_configs()
        if not changed or DRY_RUN:
            return
        docker("stop", CONTAINER)
        failures = []
        for conf in changed:
            try:
                conf.save()
            except OSError as error:
                failures.append(f"could not write {os.path.basename(conf.path)}: {error.strerror}")
                break
        try:
            docker("start", CONTAINER)
        except WiringError as error:
            failures.append(str(error))
        if failures:
            raise WiringError("; ".join(failures))

    def wait_for_web_login(self, url):
        api = Api(APP, url, {})
        deadline = time.monotonic() + float(os.environ.get("DELUGE_READY_SECONDS", "60"))
        while True:
            try:
                answer = api.request("POST", "/json", {"method": "auth.login", "params": [self.password], "id": 1})
                if answer and answer.get("result") is True:
                    return
            except WiringError:
                pass
            if time.monotonic() >= deadline:
                raise WiringError("the web UI does not accept the web password")
            time.sleep(1)


def wire():
    config = declared("apps.yml").get("deluge") or {}
    plugins = config.get("plugins") or []
    deluge = Deluge(app_secrets()["DELUGE_WEB_PASSWORD"])
    deluge.ensure_plugin_eggs(plugins)
    deluge.wire_core(config.get("core") or {}, plugins)
    deluge.wire_plugin_settings(plugins)
    deluge.wire_web_password()
    deluge.apply()
    if not DRY_RUN:
        deluge.wait_for_web_login(os.environ.get("DELUGE_URL", "http://localhost:8112"))
    if deluge.failures:
        raise WiringError("; ".join(deluge.failures))


if __name__ == "__main__":
    run(APP, wire)
