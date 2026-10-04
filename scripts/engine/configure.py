import glob
import os
import re
import shutil
import sys
from secrets import token_hex

from engine import commands, configure_menus, configure_prompts, installation, program, settings, subtitle_languages

ROTATABLE = {"sonarr": "SONARR_API_KEY", "radarr": "RADARR_API_KEY", "prowlarr": "PROWLARR_API_KEY"}
INTERNAL_CREDENTIALS = list(ROTATABLE.values())
PLAIN_KEYS = ["INSTALLATION_NAME", "TZ", "CONFIG_LOCATION", "GITHUB_OWNER", "JELLYFIN_ADMIN_USER", "RESTIC_REPOSITORY", "HOMEPAGE_PORT"]
SECRET_FILES = {
    "secrets/backup.sops.env": ["RESTIC_PASSWORD", "B2_ACCOUNT_ID", "B2_ACCOUNT_KEY"],
    "secrets/vpn.sops.env": ["VPN_SERVICE_PROVIDER", "OPENVPN_USER", "OPENVPN_PASSWORD", "SERVER_COUNTRIES"],
    "secrets/healthchecks.sops.env": ["HEALTHCHECKS_PING_KEY", "HEALTHCHECKS_API_KEY", "HEALTHCHECKS_MANAGE_KEY"],
    "secrets/apps.sops.env": [*INTERNAL_CREDENTIALS, "JELLYFIN_ADMIN_PASSWORD", "DELUGE_WEB_PASSWORD", "PORTAINER_ADMIN_PASSWORD"],
}
DOTENV_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
IDENTITY_MISSING = """git has no name or email to commit the config with; set them, then run make configure again:
  git -C {config} config user.name "Your Name"
  git -C {config} config user.email you@example.com"""


def rotated_credential(app):
    if app is None:
        return ""
    if app not in ROTATABLE:
        raise commands.Stop("can only rotate: sonarr, radarr, prowlarr")
    return ROTATABLE[app]


def git_identity_known():
    config = installation.config_dir()

    def known(variable, key):
        return bool(os.environ.get(variable)) or commands.succeeds(["git", "-C", config, "config", key])

    return known("GIT_COMMITTER_NAME", "user.name") and known("GIT_COMMITTER_EMAIL", "user.email")


def use_menus():
    chosen = os.environ.get("CONFIGURE_UI", "auto")
    if chosen in ("menus", "prompts"):
        return chosen == "menus"
    return sys.stdin.isatty() and sys.stdout.isatty() and shutil.which("whiptail") is not None


def dotenv_values(text):
    values = {}
    for line in text.splitlines():
        name, equals, value = line.partition("=")
        if equals and DOTENV_NAME.fullmatch(name):
            values[name] = value
    return values


def decrypted(path):
    return commands.output(["sops", "decrypt", "--output-type", "dotenv", path])


def current_values():
    values = {}
    if os.path.isfile("installation.env"):
        with open("installation.env") as plain:
            values.update(dotenv_values(plain.read()))
    for path in sorted(glob.glob("secrets/*.sops.env")):
        values.update(dotenv_values(decrypted(path)))
    return values


def announce_rotation(values, app, menus):
    text = f"Rotating {app.capitalize()}'s API key: a new one is generated when you save. Run make update afterwards to give it to every app that uses it."
    if menus:
        configure_menus.message(values, text, 10)
    else:
        print(text, file=sys.stderr)


def fill_internal_credentials(values, rotated):
    for name in INTERNAL_CREDENTIALS:
        if not values.get(name) or name == rotated:
            values[name] = token_hex(16)


def managed_text(names, values, current):
    managed = [f"{name}={values.get(name, '')}" for name in names]
    kept = [line for line in current.splitlines() if line and line.partition("=")[0] not in names]
    return "\n".join(managed + kept) + "\n"


def same_lines(text, other):
    return text.rstrip("\n") == other.rstrip("\n")


def write_plain(path, names, values):
    current = ""
    if os.path.isfile(path):
        with open(path) as plain:
            current = plain.read()
    text = managed_text(names, values, current)
    if not same_lines(text, current):
        with open(path, "w") as plain:
            plain.write(text)


def write_secret(path, names, values):
    current = decrypted(path) if os.path.isfile(path) else ""
    text = managed_text(names, values, current)
    if same_lines(text, current):
        return
    os.makedirs(os.path.dirname(path), exist_ok=True)
    encrypted = commands.output(
        ["sops", "encrypt", "--filename-override", path, "--input-type", "dotenv", "--output-type", "dotenv", "/dev/stdin"], input=text
    )
    with open(f"{path}.new", "w") as new:
        new.write(encrypted)
    os.replace(f"{path}.new", path)


def write_config(values):
    write_plain("installation.env", PLAIN_KEYS, values)
    subtitle_languages.write("apps.yml", values["SUBTITLE_LANGUAGES"])
    for path, names in SECRET_FILES.items():
        write_secret(path, names, values)


def commit(name):
    if not os.path.isdir(".git"):
        commands.run(["git", "init", "-q", "-b", "main"])
    commands.run(["git", "add", "-A"])
    if commands.run(["git", "diff", "--cached", "--quiet"], check=False) == 0:
        print("No changes.")
        return False
    commands.run(["git", "diff", "--cached", "--stat"])
    commands.run(["git", "commit", "-q", "-m", f"Configure {name}"])
    return True


def config_remote():
    return commands.output(["git", "remote", "get-url", "origin"], check=False, discard_errors=True).strip()


def published(values):
    repository = f"{values['GITHUB_OWNER']}/media-server-config-{values['INSTALLATION_NAME']}"
    config = installation.config_dir()
    if not commands.succeeds(["gh", "auth", "status"]):
        print("Keeping the config on GitHub needs the GitHub CLI logged in: run gh auth login.", file=sys.stderr)
        return False
    if commands.succeeds(["gh", "repo", "view", repository]):
        print(
            f"{repository} already exists on GitHub. If it is this config's, empty or not, use it with: "
            f'git -C "{config}" remote add origin git@github.com:{repository}.git && git -C "{config}" push -u origin main',
            file=sys.stderr,
        )
        return False
    if commands.run(["gh", "repo", "create", repository, "--private", "--source", ".", "--push"], check=False) != 0:
        return False
    print(
        f"Published to https://github.com/{repository}. Add it to the Renovate app so image and engine updates arrive as pull requests, "
        "and add the engine repository too if it is private: https://github.com/apps/renovate"
    )
    return True


def not_published(from_create):
    print(
        f"The settings are saved and committed in {installation.config_dir()}, but not on GitHub yet. Fix the above, then run make configure again to publish them.",
        file=sys.stderr,
    )
    if not from_create:
        raise commands.CommandFailed(1)


def push():
    if commands.run(["git", "push", "-q", "origin", "HEAD"], check=False) == 0:
        print("Committed and pushed. Run make update to apply it here; the other machines apply it at their next update.")
        return
    print("Committed here, but the push to GitHub failed (see above), so the other machines do not have it yet.", file=sys.stderr)
    print(
        f'If this machine can only read the config, give its deploy key write access on GitHub, then push with: git -C "{installation.config_dir()}" push',
        file=sys.stderr,
    )
    raise commands.CommandFailed(1)


def apply_location(values, committed, from_create):
    remote = config_remote()
    on_github = values["CONFIG_LOCATION"] == "github"
    if on_github and not remote:
        if not published(values):
            not_published(from_create)
    elif on_github and committed:
        push()
    elif not on_github and committed and not from_create:
        print("Committed. Run make update to apply it.")
    if not on_github and remote:
        commands.run(["git", "remote", "remove", "origin"])
        print(f"This config is now local only. The repository at {remote} is kept; delete it there if you no longer need it.")


def configure(name=None, rotate=None, from_create=False):
    rotated = rotated_credential(rotate)
    config = installation.config_dir()
    if not os.path.isfile(os.path.join(config, ".sops.yaml")):
        raise installation.not_an_installation()
    if not git_identity_known():
        raise commands.Stop(IDENTITY_MISSING.format(config=config))
    os.chdir(config)
    if not os.path.isfile("images.yml"):
        shutil.copytree(os.path.join(installation.engine_dir(), "config-template"), ".", dirs_exist_ok=True)
    new = not os.path.isfile("installation.env")
    values = current_values()
    settings.split_repository(values)
    values["INSTALLATION_NAME"] = values.get("INSTALLATION_NAME") or name or ""
    if not values["INSTALLATION_NAME"]:
        raise commands.Stop("this config has no installation name; create one with make create-installation NAME=<name>")
    menus = use_menus()
    if rotated:
        announce_rotation(values, rotate, menus)
    if menus:
        if not configure_menus.fill(values, new):
            print("Nothing changed.")
            return
    else:
        configure_prompts.fill(values)
    settings.join_repository(values)
    fill_internal_credentials(values, rotated)
    write_config(values)
    apply_location(values, commit(values["INSTALLATION_NAME"]), from_create)


def run(argv):
    rotate = None
    if argv[:1] == ["--rotate"]:
        rotate = argv[1] if len(argv) > 1 else ""
    configure(name=os.environ.get("NAME"), rotate=rotate)


def main(argv):
    return program.run("configure", run, argv)
