import io
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest
import yaml

from conftest import REPO, done, fresh_engine, guided_answers, real, replied, whiptail, whiptail_screens

NEW_INSTALLATION = [
    "Europe/London", "github", "someone", "admin", "b2", "testinst-media-server-backup", "restic", "0031keyid", "K005applicationkey",
    "restic-password-typed", "protonvpn", "vpn-user", "vpn-password", "Ireland", "hc-ping-key-12345", "hc-read-only-api-key",
    "hc-read-write-api-key", "jellyfin-pass", "deluge-pass", "portainer-pass-long", "en",
]
LOCAL_ONLY = NEW_INSTALLATION[:1] + ["local"] + NEW_INSTALLATION[3:]
OPENVPN_PASSWORD_QUESTION = 13


def answers(*lines):
    return "".join(f"{line}\n" for line in lines)


def enter_on_every_prompt():
    return answers(*[""] * 23)


def changing(position, answer):
    return answers(*[answer if question == position else "" for question in range(1, 23)])


def with_backup(kind, location):
    return NEW_INSTALLATION[:4] + [kind, location] + NEW_INSTALLATION[9:]


def sops_decrypts(args, input):
    return "".join(line[len("ENC:"):] if line.startswith("ENC:") else line for line in Path(args[-1]).read_text().splitlines(keepends=True))


def sops_encrypts(args, input):
    return "".join(f"ENC:{line}" for line in input.splitlines(keepends=True))


def creates_the_repository(commands):
    repository = commands.ran[-1].args[3]
    commands.start(["git", "remote", "add", "origin", f"git@github.com:{repository}.git"]).wait()


@pytest.fixture
def ports_in_use():
    return set()


@pytest.fixture
def configure(dirs, commands, ports_in_use, tmp_path, monkeypatch):
    for name, value in {
        "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
        "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_SYSTEM": os.devnull, "HOME": str(tmp_path / "home"),
    }.items():
        monkeypatch.setenv(name, value)
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ["NAME"] = "testinst"
    for name in ("PROMPT_INPUT_ENDED", "CONFIGURE_UI", "BACKUP_CHECK_SECONDS"):
        os.environ.pop(name, None)
    monkeypatch.chdir(tmp_path)
    shutil.copytree(REPO / "config-template", dirs.engine / "config-template")
    (dirs.config / ".sops.yaml").write_text("creation_rules:\n  - path_regex: secrets/\n    age: age1test\n")
    commands.on(["git"], real())
    commands.on(["git", "push"])
    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="git@github.com:someone/media-server-engine.git\n"))
    commands.on(["sops", "decrypt"], replied(sops_decrypts))
    commands.on(["sops", "encrypt"], replied(sops_encrypts))
    commands.on(["gh", "auth", "status"])
    commands.on(["gh", "repo", "view"], done(returncode=1))
    commands.on(["gh", "repo", "create"], done(then=lambda: creates_the_repository(commands)))
    commands.on(["restic"], done(returncode=10))
    monkeypatch.setattr(shutil, "which", lambda name, *args, **kwargs: f"/usr/bin/{name}" if name == "restic" else None)
    module = fresh_engine("engine.configure")
    monkeypatch.setattr(module.settings.ports, "port_in_use", lambda port: port in ports_in_use)
    return module


def run(configure, monkeypatch, typed, *argv):
    monkeypatch.setattr(sys, "stdin", io.StringIO(typed))
    os.environ.pop("PROMPT_INPUT_ENDED", None)
    return configure.main(list(argv))


def said(capsys):
    output = capsys.readouterr()
    return output.out + output.err


def lines_of(path):
    return path.read_text().splitlines()


def git(commands, config, *args):
    output, _ = commands.start(["git", "-C", str(config), *args], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True).communicate()
    return output.strip()


def commits(commands, config):
    return int(git(commands, config, "rev-list", "--count", "HEAD") or 0)


def files_of(config):
    return {str(path.relative_to(config)): path.read_bytes() for path in config.rglob("*") if path.is_file() and ".git" not in path.relative_to(config).parts}


def test_configure_fills_a_new_config_from_the_answers_and_commits(configure, commands, dirs, monkeypatch):
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 0
    assert lines_of(dirs.config / "installation.env") == [
        "INSTALLATION_NAME=testinst", "TZ=Europe/London", "CONFIG_LOCATION=github", "GITHUB_OWNER=someone", "JELLYFIN_ADMIN_USER=admin",
        "RESTIC_REPOSITORY=b2:testinst-media-server-backup:restic", "HOMEPAGE_PORT=80",
    ]
    assert "ENC:OPENVPN_PASSWORD=vpn-password" in lines_of(dirs.config / "secrets" / "vpn.sops.env")
    assert "ENC:B2_ACCOUNT_KEY=K005applicationkey" in lines_of(dirs.config / "secrets" / "backup.sops.env")
    assert lines_of(dirs.config / "secrets" / "healthchecks.sops.env") == [
        "ENC:HEALTHCHECKS_PING_KEY=hc-ping-key-12345", "ENC:HEALTHCHECKS_API_KEY=hc-read-only-api-key", "ENC:HEALTHCHECKS_MANAGE_KEY=hc-read-write-api-key",
    ]
    assert (dirs.config / "images.yml").is_file()
    assert commits(commands, dirs.config) == 1


def test_configure_keeps_secrets_out_of_plain_files(configure, dirs, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    plain = [content for name, content in files_of(dirs.config).items() if not name.endswith(".sops.env")]
    assert not any(secret in content for content in plain for secret in (b"vpn-password", b"K005applicationkey", b"jellyfin-pass"))


def test_configure_generates_internal_credentials_and_never_shows_them(configure, dirs, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    apps = lines_of(dirs.config / "secrets" / "apps.sops.env")
    for name in ("SONARR_API_KEY", "RADARR_API_KEY", "PROWLARR_API_KEY"):
        assert any(re.fullmatch(f"ENC:{name}=[0-9a-f]{{32}}", line) for line in apps)
    sonarr_key = next(line for line in apps if line.startswith("ENC:SONARR_API_KEY=")).partition("=")[2]
    assert sonarr_key not in said(capsys)
    assert not any("DELUGE_DAEMON" in line for line in apps)


def test_configure_run_again_with_only_enter_changes_nothing_and_commits_nothing(configure, commands, dirs, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    before = files_of(dirs.config)
    capsys.readouterr()
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 0
    assert files_of(dirs.config) == before
    assert commits(commands, dirs.config) == 1
    assert "No changes." in said(capsys)


def test_unchanged_secrets_are_not_encrypted_again_when_sops_leaves_out_the_last_newline(configure, commands, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    encrypted = commands.count("sops", "encrypt")
    commands.on(["sops", "decrypt"], replied(lambda args, input: sops_decrypts(args, input).rstrip("\n")))
    run(configure, monkeypatch, enter_on_every_prompt())
    assert commands.count("sops", "encrypt") == encrypted


def test_configure_shows_current_values_as_defaults_and_masks_secrets(configure, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    capsys.readouterr()
    run(configure, monkeypatch, enter_on_every_prompt())
    output = said(capsys)
    assert "Installation: testinst" in output
    assert "B2 application key [set, ends …key]" in output
    assert "K005applicationkey" not in output


def test_the_openvpn_user_is_masked_like_a_secret(configure, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    capsys.readouterr()
    run(configure, monkeypatch, enter_on_every_prompt())
    output = said(capsys)
    assert "vpn-user" not in output
    assert "OpenVPN user [set, ends …ser]" in output


def test_changing_one_secret_rewrites_only_its_file(configure, commands, dirs, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    run(configure, monkeypatch, changing(OPENVPN_PASSWORD_QUESTION, "new-vpn-password"))
    assert commits(commands, dirs.config) == 2
    assert git(commands, dirs.config, "show", "--name-only", "--format=", "HEAD") == "secrets/vpn.sops.env"
    assert "ENC:OPENVPN_PASSWORD=new-vpn-password" in lines_of(dirs.config / "secrets" / "vpn.sops.env")


def test_keys_added_to_a_secrets_file_by_hand_survive(configure, dirs, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    with open(dirs.config / "secrets" / "vpn.sops.env", "a") as vpn:
        vpn.write("ENC:WIREGUARD_MTU=1320\n")
    assert run(configure, monkeypatch, changing(OPENVPN_PASSWORD_QUESTION, "new-vpn-password")) == 0
    assert {"ENC:WIREGUARD_MTU=1320", "ENC:OPENVPN_PASSWORD=new-vpn-password"} <= set(lines_of(dirs.config / "secrets" / "vpn.sops.env"))


def test_reconfiguring_keeps_keys_and_comments_added_to_installation_env_by_hand(configure, dirs, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    with open(dirs.config / "installation.env", "a") as plain:
        plain.write("# the name the apps are reached by=media.local\nMEDIA_SERVER_HOST=media.local\n")
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 0
    assert {"MEDIA_SERVER_HOST=media.local", "# the name the apps are reached by=media.local", "TZ=Europe/London"} <= set(lines_of(dirs.config / "installation.env"))


@pytest.mark.parametrize("password", ["passYWo=", "pass=word=="])
def test_secrets_ending_in_equals_signs_are_kept_exactly(configure, dirs, monkeypatch, password):
    run(configure, monkeypatch, answers(*[password if answer == "restic-password-typed" else answer for answer in NEW_INSTALLATION]))
    assert f"ENC:RESTIC_PASSWORD={password}" in lines_of(dirs.config / "secrets" / "backup.sops.env")
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 0
    assert f"ENC:RESTIC_PASSWORD={password}" in lines_of(dirs.config / "secrets" / "backup.sops.env")


def test_rotating_regenerates_one_internal_credential_only(configure, dirs, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))

    def keys():
        return dict(line[len("ENC:"):].partition("=")[::2] for line in lines_of(dirs.config / "secrets" / "apps.sops.env"))

    before = keys()
    assert run(configure, monkeypatch, enter_on_every_prompt(), "--rotate", "sonarr") == 0
    after = keys()
    assert after["SONARR_API_KEY"] != before["SONARR_API_KEY"]
    assert after["RADARR_API_KEY"] == before["RADARR_API_KEY"]


def test_rotating_says_what_it_rotates_and_that_make_update_applies_it(configure, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*LOCAL_ONLY))
    capsys.readouterr()
    assert run(configure, monkeypatch, enter_on_every_prompt(), "--rotate", "sonarr") == 0
    output = said(capsys)
    assert "Rotating Sonarr's API key: a new one is generated when you save. Run make update afterwards to give it to every app that uses it." in output
    assert "Committed. Run make update to apply it." in output


@pytest.mark.parametrize("app", [["deluge"], ["jellyfin"], []])
def test_rotating_anything_but_sonarr_radarr_or_prowlarr_is_refused(configure, monkeypatch, capsys, app):
    assert run(configure, monkeypatch, enter_on_every_prompt(), "--rotate", *app) == 1
    assert capsys.readouterr().err == "configure: can only rotate: sonarr, radarr, prowlarr\n"


def test_a_new_config_kept_on_github_is_published_as_a_private_repo_not_pushed(configure, commands, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 0
    assert commands.did("gh", "repo", "create", "someone/media-server-config-testinst", "--private", "--source", ".", "--push")
    assert not commands.did("git", "push")
    assert "CONFIG_LOCATION=github" in lines_of(dirs.config / "installation.env")
    assert "https://github.com/apps/renovate" in said(capsys)


def test_a_change_to_a_config_on_github_is_pushed_once_committed(configure, commands, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    capsys.readouterr()
    assert run(configure, monkeypatch, changing(OPENVPN_PASSWORD_QUESTION, "new-vpn-password")) == 0
    assert commands.did("git", "push", "-q", "origin", "HEAD")
    assert "Committed and pushed. Run make update to apply it here; the other machines apply it at their next update." in said(capsys)


def test_a_push_that_fails_keeps_the_commit_says_why_and_fails(configure, commands, dirs, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    commands.on(["git", "push"], done(returncode=1))
    assert run(configure, monkeypatch, changing(OPENVPN_PASSWORD_QUESTION, "new-vpn-password")) == 1
    assert commits(commands, dirs.config) == 2
    assert "give its deploy key write access on GitHub" in said(capsys)


def test_a_local_only_config_never_touches_github_and_is_not_asked_for_an_owner(configure, commands, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*LOCAL_ONLY)) == 0
    assert not commands.did("gh", "repo")
    assert git(commands, dirs.config, "remote", "get-url", "origin") == ""
    assert {"CONFIG_LOCATION=local", "JELLYFIN_ADMIN_USER=admin"} <= set(lines_of(dirs.config / "installation.env"))
    assert commits(commands, dirs.config) == 1
    output = said(capsys)
    assert "GitHub owner" not in output
    assert "Committed. Run make update to apply it." in output


def test_a_local_config_is_published_when_switched_to_github(configure, commands, dirs, monkeypatch):
    run(configure, monkeypatch, answers(*LOCAL_ONLY))
    assert run(configure, monkeypatch, answers("", "github", "someone", *[""] * 20)) == 0
    assert commands.did("gh", "repo", "create", "someone/media-server-config-testinst", "--private", "--source", ".", "--push")
    assert git(commands, dirs.config, "remote", "get-url", "origin") == "git@github.com:someone/media-server-config-testinst.git"


def test_a_config_switched_to_local_stops_pulling_from_github_and_keeps_the_repo(configure, commands, dirs, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    capsys.readouterr()
    assert run(configure, monkeypatch, answers("", "local", *[""] * 20)) == 0
    assert git(commands, dirs.config, "remote", "get-url", "origin") == ""
    assert not commands.did("gh", "repo", "delete")
    assert "This config is now local only. The repository at git@github.com:someone/media-server-config-testinst.git is kept" in said(capsys)


def test_publishing_refuses_a_github_repo_that_already_exists(configure, commands, monkeypatch, capsys):
    commands.on(["gh", "repo", "view"])
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 1
    assert "someone/media-server-config-testinst already exists on GitHub." in said(capsys)
    assert not commands.did("gh", "repo", "create")


def test_publishing_needs_the_github_cli_logged_in(configure, commands, monkeypatch, capsys):
    commands.on(["gh", "auth", "status"], done(returncode=1))
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 1
    output = said(capsys)
    assert "run gh auth login" in output
    assert "but not on GitHub yet" in output


def test_a_publish_that_fails_by_hand_fails_the_run(configure, commands, monkeypatch, capsys):
    commands.on(["gh", "repo", "create"], done(returncode=1))
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 1
    assert "but not on GitHub yet" in said(capsys)


@pytest.mark.parametrize("failure", [["gh", "auth", "status"], ["gh", "repo", "create"]])
def test_a_publish_that_fails_while_creating_keeps_the_settings_and_says_how_to_publish_later(configure, commands, dirs, monkeypatch, capsys, failure):
    commands.on(failure, done(returncode=1))
    monkeypatch.setattr(sys, "stdin", io.StringIO(answers(*NEW_INSTALLATION)))
    configure.configure("testinst", from_create=True)
    assert commits(commands, dirs.config) == 1
    assert "but not on GitHub yet. Fix the above, then run make configure again to publish them." in said(capsys)


def test_a_saved_local_config_from_create_does_not_say_to_run_make_update(configure, monkeypatch, capsys):
    monkeypatch.setattr(sys, "stdin", io.StringIO(answers(*LOCAL_ONLY)))
    configure.configure("testinst", from_create=True)
    assert "Run make update" not in said(capsys)


def test_configure_stops_before_asking_anything_when_git_has_no_name_or_email(configure, monkeypatch, capsys):
    for name in ("GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"):
        os.environ.pop(name)
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 1
    output = said(capsys)
    assert "configure: git has no name or email to commit the config with" in output
    assert "config user.email you@example.com" in output
    assert "Time zone" not in output


def test_configure_refuses_a_config_without_sops_yaml(configure, dirs, monkeypatch, capsys):
    (dirs.config / ".sops.yaml").unlink()
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 1
    assert "To create one: make create-installation NAME=<name>" in said(capsys)


def test_configure_outside_an_installation_creates_nothing_and_says_so(configure, dirs, monkeypatch, capsys):
    shutil.rmtree(dirs.config)
    os.environ["NAME"] = ""
    assert run(configure, monkeypatch, "") == 1
    assert not dirs.config.exists()
    assert "is not an installation" in said(capsys)


def test_the_installation_name_is_never_asked_and_comes_from_the_config_once_saved(configure, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 0
    assert "Installation name [" not in said(capsys)
    os.environ["NAME"] = ""
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 0
    assert "Installation: testinst" in said(capsys)


def test_configure_without_an_installation_name_refuses(configure, monkeypatch, capsys):
    os.environ["NAME"] = ""
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 1
    assert capsys.readouterr().err.endswith("configure: this config has no installation name; create one with make create-installation NAME=<name>\n")


def test_configure_keeps_the_files_of_a_config_that_already_has_the_template(configure, dirs, monkeypatch):
    shutil.copytree(REPO / "config-template", dirs.config, dirs_exist_ok=True)
    (dirs.config / "engine.env").write_text("ENGINE_VERSION=local\n")
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 0
    assert lines_of(dirs.config / "engine.env") == ["ENGINE_VERSION=local"]


def test_a_portainer_password_shorter_than_12_characters_is_asked_again(configure, dirs, monkeypatch, capsys):
    typed = [answer for answer in NEW_INSTALLATION for answer in (["short", answer] if answer == "portainer-pass-long" else [answer])]
    assert run(configure, monkeypatch, answers(*typed)) == 0
    assert "Portainer needs at least 12 characters." in said(capsys)
    assert "ENC:PORTAINER_ADMIN_PASSWORD=portainer-pass-long" in lines_of(dirs.config / "secrets" / "apps.sops.env")


def test_the_backups_are_a_choice_of_a_local_folder_backblaze_b2_or_another_repository(configure, dirs, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    output = said(capsys)
    assert "    local: A folder on this machine or on a mounted disk" in output
    assert "    b2: Backblaze B2" in output
    assert "Where to keep the backups (local/b2/other) [local]: " in output


def test_a_local_backup_folder_is_asked_for_created_and_needs_no_b2_keys(configure, dirs, tmp_path, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*with_backup("local", "~/testinst-backups"))) == 0
    assert "B2 key ID" not in said(capsys)
    assert f"RESTIC_REPOSITORY={tmp_path}/home/testinst-backups" in lines_of(dirs.config / "installation.env")
    assert (tmp_path / "home" / "testinst-backups").is_dir()
    assert "ENC:RESTIC_PASSWORD=restic-password-typed" in lines_of(dirs.config / "secrets" / "backup.sops.env")


def test_a_backup_folder_that_cannot_be_made_is_explained_and_can_be_kept_anyway(configure, dirs, tmp_path, monkeypatch, capsys):
    if os.geteuid() == 0:
        pytest.skip("root can write into any folder")
    readonly = tmp_path / "readonly"
    readonly.mkdir()
    readonly.chmod(0o555)
    typed = with_backup("local", f"{readonly}/backups")
    try:
        assert run(configure, monkeypatch, answers(*typed[:7], "y", *typed[7:])) == 0
    finally:
        readonly.chmod(0o755)
    assert "cannot be created or written to" in said(capsys)
    assert f"RESTIC_REPOSITORY={readonly}/backups" in lines_of(dirs.config / "installation.env")


def test_b2_details_that_do_not_open_the_existing_backups_are_explained_and_asked_again(configure, commands, monkeypatch, capsys):
    commands.on(["restic"], done(returncode=12))
    b2_again = ["n", "b2", "testinst-media-server-backup", "restic", "0031keyid", "K005applicationkey", "restic-password-typed", "y"]
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION[:10], *b2_again, *NEW_INSTALLATION[10:])) == 0
    assert said(capsys).count("does not open the backups") == 2


def test_answers_that_run_out_while_the_backup_keeps_failing_stop_configure(configure, commands, monkeypatch, capsys):
    commands.on(["restic"], done(returncode=12))
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION[:10])) == 1
    assert "configure: the answers ran out before the backup details were settled" in said(capsys)


def test_a_time_zone_that_does_not_exist_is_asked_again(configure, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers("Mars/Olympus", *NEW_INSTALLATION)) == 0
    assert "Mars/Olympus is not a time zone. Use a name such as Europe/London." in said(capsys)
    assert "TZ=Europe/London" in lines_of(dirs.config / "installation.env")


def test_another_kind_of_restic_repository_is_used_as_typed(configure, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*with_backup("other", "s3:s3.amazonaws.com/testinst-bucket/restic"))) == 0
    assert "B2 key ID" not in said(capsys)
    assert "RESTIC_REPOSITORY=s3:s3.amazonaws.com/testinst-bucket/restic" in lines_of(dirs.config / "installation.env")


@pytest.mark.parametrize("repository", ["sftp:backup@nas:/srv/restic", "b2:testinst-bucket"])
def test_reconfiguring_keeps_a_saved_restic_repository(configure, dirs, monkeypatch, repository):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    plain = dirs.config / "installation.env"
    plain.write_text(re.sub(r"(?m)^RESTIC_REPOSITORY=.*$", f"RESTIC_REPOSITORY={repository}", plain.read_text()))
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 0
    assert f"RESTIC_REPOSITORY={repository}" in lines_of(plain)


def test_subtitle_languages_default_to_english_and_are_written_to_apps_yml(configure, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION[:-1], "en, es")) == 0
    assert yaml.safe_load((dirs.config / "apps.yml").read_text())["bazarr"]["languages"] == ["en", "es"]
    capsys.readouterr()
    run(configure, monkeypatch, enter_on_every_prompt())
    assert "Subtitle languages (codes, comma separated) [en, es]: " in said(capsys)


def test_a_new_installation_is_asked_only_for_the_landing_pages_port(configure, dirs, monkeypatch, capsys):
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 0
    output = said(capsys)
    assert "Landing page port" in output
    assert not re.search("Theme|Colour|Tiles to leave off", output)
    assert "homepage:" not in (dirs.config / "apps.yml").read_text()
    assert "HOMEPAGE_PORT=80" in lines_of(dirs.config / "installation.env")


def test_with_port_80_taken_the_landing_page_is_offered_the_first_free_port_saying_why(configure, dirs, ports_in_use, monkeypatch, capsys):
    ports_in_use.update({80, 8080})
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION)) == 0
    assert "Port 80 is in use on this machine" in said(capsys)
    assert "HOMEPAGE_PORT=8081" in lines_of(dirs.config / "installation.env")


@pytest.mark.parametrize("refused, problem", [("9000", "Port 9000 is in use on this machine"), ("http", "a port number")])
def test_a_landing_page_port_that_cannot_be_used_is_explained_and_asked_again(configure, dirs, ports_in_use, monkeypatch, capsys, refused, problem):
    ports_in_use.add(9000)
    assert run(configure, monkeypatch, answers(*NEW_INSTALLATION, refused, "8090")) == 0
    assert problem in said(capsys)
    assert "HOMEPAGE_PORT=8090" in lines_of(dirs.config / "installation.env")


def test_every_secret_the_configarr_template_uses_is_one_configure_writes(configure):
    used = set(re.findall(r"!secret ([A-Z_]+)", (REPO / "config-template" / "configarr" / "config.yml").read_text()))
    assert used and used <= set(configure.SECRET_FILES["secrets/apps.sops.env"])


@pytest.mark.parametrize(
    "ui, terminal, whiptail_installed, menus",
    [("menus", False, False, True), ("prompts", True, True, False), (None, True, True, True), (None, False, True, False), (None, True, False, False)],
)
def test_menus_are_used_on_a_terminal_with_whiptail_unless_chosen_otherwise(configure, monkeypatch, ui, terminal, whiptail_installed, menus):
    if ui:
        os.environ["CONFIGURE_UI"] = ui
    monkeypatch.setattr(sys, "stdin", SimpleNamespace(isatty=lambda: terminal))
    monkeypatch.setattr(sys, "stdout", SimpleNamespace(isatty=lambda: terminal))
    monkeypatch.setattr(shutil, "which", lambda name, *args, **kwargs: "/usr/bin/whiptail" if whiptail_installed else None)
    assert configure.use_menus() == menus


def test_a_new_installation_set_up_in_the_menus_is_saved_and_committed(configure, commands, dirs, tmp_path, monkeypatch):
    os.environ["CONFIGURE_UI"] = "menus"
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    assert run(configure, monkeypatch, "") == 0
    assert {"CONFIG_LOCATION=local", f"RESTIC_REPOSITORY={tmp_path}/backups"} <= set(lines_of(dirs.config / "installation.env"))
    assert "ENC:JELLYFIN_ADMIN_PASSWORD=jelly-typed" in lines_of(dirs.config / "secrets" / "apps.sops.env")
    assert commits(commands, dirs.config) == 1


def test_one_change_in_the_menus_touches_only_its_file(configure, commands, dirs, tmp_path, monkeypatch):
    os.environ["CONFIGURE_UI"] = "menus"
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    run(configure, monkeypatch, "")
    whiptail(commands, "0|VPN", "0|OPENVPN_PASSWORD", "0|new-vpn-password", "0|Back", "0|Save")
    assert run(configure, monkeypatch, "") == 0
    assert commits(commands, dirs.config) == 2
    assert git(commands, dirs.config, "show", "--name-only", "--format=", "HEAD") == "secrets/vpn.sops.env"


def test_discarding_in_the_menus_leaves_everything_as_it_was(configure, commands, dirs, tmp_path, monkeypatch, capsys):
    os.environ["CONFIGURE_UI"] = "menus"
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    run(configure, monkeypatch, "")
    before = files_of(dirs.config)
    capsys.readouterr()
    whiptail(commands, "0|VPN", "0|OPENVPN_USER", "0|someone-else", "0|Back", "0|Discard")
    assert run(configure, monkeypatch, "") == 0
    assert files_of(dirs.config) == before
    assert commits(commands, dirs.config) == 1
    assert "Nothing changed." in said(capsys)


def test_rotating_in_the_menus_says_what_it_rotates_in_a_box(configure, commands, tmp_path, monkeypatch, capsys):
    os.environ["CONFIGURE_UI"] = "menus"
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    run(configure, monkeypatch, "")
    capsys.readouterr()
    whiptail(commands, "0|Save")
    assert run(configure, monkeypatch, "", "--rotate", "radarr") == 0
    assert any("--msgbox Rotating Radarr's API key" in screen for screen in whiptail_screens(commands))
    assert "Run make update" in said(capsys)


def test_configuring_from_create_leaves_the_working_directory_as_it_was(configure, tmp_path, monkeypatch):
    monkeypatch.setattr(sys, "stdin", io.StringIO(answers(*LOCAL_ONLY)))
    configure.configure("testinst", from_create=True)
    assert os.path.realpath(os.getcwd()) == os.path.realpath(tmp_path)


def test_the_github_owner_create_checked_is_the_one_offered(configure, commands, dirs, monkeypatch):
    monkeypatch.setattr(sys, "stdin", io.StringIO(answers(*NEW_INSTALLATION[:2], "", *NEW_INSTALLATION[3:])))
    configure.configure("testinst", from_create=True, github_owner="org")
    assert commands.did("gh", "repo", "create", "org/media-server-config-testinst")
    assert "GITHUB_OWNER=org" in lines_of(dirs.config / "installation.env")


def test_an_answer_that_is_not_a_choice_is_asked_again_offering_the_saved_one(configure, commands, dirs, monkeypatch, capsys):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    capsys.readouterr()
    assert run(configure, monkeypatch, answers("", "GitHub", *[""] * 22)) == 0
    assert "Where to keep this config (local/github) [github]: \nChoose one of: local github.\nWhere to keep this config (local/github) [github]: " in said(capsys)
    assert "CONFIG_LOCATION=github" in lines_of(dirs.config / "installation.env")
    assert git(commands, dirs.config, "remote", "get-url", "origin") == "git@github.com:someone/media-server-config-testinst.git"


def test_a_saved_landing_page_port_now_in_use_is_asked_again_offering_a_free_one(configure, dirs, ports_in_use, monkeypatch):
    run(configure, monkeypatch, answers(*NEW_INSTALLATION))
    ports_in_use.add(80)
    assert run(configure, monkeypatch, enter_on_every_prompt()) == 0
    assert "HOMEPAGE_PORT=8080" in lines_of(dirs.config / "installation.env")
