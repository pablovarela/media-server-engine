import os
import pwd
import shutil

import pytest

from conftest import done, fresh_engine, timed_out

CONDITIONAL = {
    "GITHUB_OWNER": ("CONFIG_LOCATION", "github"),
    "BACKUP_FOLDER": ("BACKUP_TYPE", "local"),
    "BACKUP_URL": ("BACKUP_TYPE", "other"),
    "B2_BUCKET": ("BACKUP_TYPE", "b2"),
    "B2_FOLDER": ("BACKUP_TYPE", "b2"),
    "B2_ACCOUNT_ID": ("BACKUP_TYPE", "b2"),
    "B2_ACCOUNT_KEY": ("BACKUP_TYPE", "b2"),
}
B2 = {"BACKUP_TYPE": "b2", "RESTIC_REPOSITORY": "b2:bucket:restic", "B2_ACCOUNT_ID": "0031keyid", "B2_ACCOUNT_KEY": "K005key", "RESTIC_PASSWORD": "restic-typed"}
PORT_RANGE = "The landing page port must be a port number, from 1 to 65535."


@pytest.fixture
def ports_in_use():
    return set()


@pytest.fixture
def settings(dirs, commands, ports_in_use, tmp_path, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ["HOME"] = str(tmp_path / "home")
    os.environ.pop("BACKUP_CHECK_SECONDS", None)
    monkeypatch.chdir(dirs.config)
    monkeypatch.setattr(shutil, "which", lambda name, *args, **kwargs: f"/usr/bin/{name}")
    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="git@github.com:someone/media-server-engine.git\n"))
    module = fresh_engine("engine.settings")
    monkeypatch.setattr(module.ports, "port_in_use", lambda port: port in ports_in_use)
    return module


def new():
    return {"INSTALLATION_NAME": "testinst"}


def test_a_new_installation_is_offered_these_defaults(settings, tmp_path):
    asked_with_a_default = [field.name for field in settings.FIELDS if field.kind in ("text", "choice", "timezone")]
    assert {name: settings.default(name, new()) for name in asked_with_a_default} == {
        "TZ": "Etc/UTC", "CONFIG_LOCATION": "local", "GITHUB_OWNER": "someone", "JELLYFIN_ADMIN_USER": "admin", "BACKUP_TYPE": "local",
        "BACKUP_FOLDER": f"{tmp_path}/home/testinst-backups", "BACKUP_URL": "", "B2_BUCKET": "testinst-media-server-backup", "B2_FOLDER": "restic",
        "B2_ACCOUNT_ID": "", "VPN_SERVICE_PROVIDER": "", "SERVER_COUNTRIES": "", "SUBTITLE_LANGUAGES": "en", "HOMEPAGE_PORT": "80",
    }


def test_saved_values_are_offered_instead_and_a_b2_bucket_saved_without_a_folder_keeps_none(settings):
    values = {**new(), "TZ": "Europe/London", "B2_FOLDER": "", "HOMEPAGE_PORT": "8080"}
    assert [settings.default(name, values) for name in ("TZ", "B2_FOLDER", "HOMEPAGE_PORT")] == ["Europe/London", "", "8080"]


def test_the_subtitle_languages_offered_are_those_in_apps_yml(settings, dirs):
    (dirs.config / "apps.yml").write_text("bazarr:\n  languages: [en, es]\n")
    assert settings.default("SUBTITLE_LANGUAGES", new()) == "en, es"


def test_an_engine_cloned_from_elsewhere_than_github_offers_no_owner(settings, commands, dirs):
    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="/srv/git/media-server-engine\n"))
    assert settings.default("GITHUB_OWNER", new()) == ""


def test_with_port_80_taken_the_first_free_port_is_offered_and_the_help_says_why(settings, ports_in_use):
    ports_in_use.update({80, 8080})
    assert settings.default("HOMEPAGE_PORT", new()) == "8081"
    assert settings.help_text("HOMEPAGE_PORT", new()) == "Port 80 is in use on this machine, so a free one is suggested. The page is at http://<machine>:<port>."


def test_with_a_saved_port_the_help_only_says_where_the_page_is(settings, ports_in_use):
    ports_in_use.add(80)
    assert settings.help_text("HOMEPAGE_PORT", {**new(), "HOMEPAGE_PORT": "80"}) == "The page is at http://<machine>, with :<port> unless the port is 80."


def test_with_every_landing_page_port_taken_80_is_offered(settings, ports_in_use):
    ports_in_use.update({80, *range(8080, 8100)})
    assert settings.default("HOMEPAGE_PORT", new()) == "80"


@pytest.mark.parametrize("name", sorted(CONDITIONAL))
def test_some_fields_are_asked_only_for_one_choice(settings, name):
    setting, wanted = CONDITIONAL[name]
    assert settings.applies(name, {setting: wanted})
    assert not settings.applies(name, {setting: "something-else"})


def test_every_other_field_is_always_asked(settings):
    assert all(settings.applies(field.name, {}) for field in settings.FIELDS if field.name not in CONDITIONAL)


@pytest.mark.parametrize(
    "repository, fields",
    [
        ("b2:bucket:restic", {"BACKUP_TYPE": "b2", "B2_BUCKET": "bucket", "B2_FOLDER": "restic"}),
        ("b2:bucket", {"BACKUP_TYPE": "b2", "B2_BUCKET": "bucket", "B2_FOLDER": ""}),
        ("/srv/backups", {"BACKUP_TYPE": "local", "BACKUP_FOLDER": "/srv/backups"}),
        ("sftp:backup@nas:/srv/restic", {"BACKUP_TYPE": "other", "BACKUP_URL": "sftp:backup@nas:/srv/restic"}),
    ],
)
def test_a_restic_repository_splits_into_its_fields_and_joins_back_as_it_was(settings, repository, fields):
    values = {"RESTIC_REPOSITORY": repository}
    settings.split_repository(values)
    assert values == {"RESTIC_REPOSITORY": repository, **fields}
    values["RESTIC_REPOSITORY"] = ""
    settings.join_repository(values)
    assert values["RESTIC_REPOSITORY"] == repository


def test_no_restic_repository_leaves_the_backup_fields_unset(settings):
    values = {}
    settings.split_repository(values)
    assert values == {}


def test_a_backup_folder_can_start_with_a_tilde_for_the_home_folder(settings, tmp_path):
    assert settings.normalize("BACKUP_FOLDER", "~/backups") == f"{tmp_path}/home/backups"


def test_an_empty_landing_page_port_takes_the_first_free_one(settings, ports_in_use):
    ports_in_use.add(80)
    assert settings.normalize("HOMEPAGE_PORT", "") == "8080"


@pytest.mark.parametrize(
    "name, value, problem",
    [
        ("TZ", "Mars/Olympus", "Mars/Olympus is not a time zone. Use a name such as Europe/London."),
        ("TZ", "Europe", "Europe is not a time zone. Use a name such as Europe/London."),
        ("TZ", "Europe/London", ""),
        ("HOMEPAGE_PORT", "http", PORT_RANGE),
        ("HOMEPAGE_PORT", "0", PORT_RANGE),
        ("HOMEPAGE_PORT", "65536", PORT_RANGE),
        ("HOMEPAGE_PORT", "8090", ""),
        ("CONFIG_LOCATION", "GitHub", "Choose one of: local github."),
        ("CONFIG_LOCATION", "github", ""),
        ("BACKUP_TYPE", "s3", "Choose one of: local b2 other."),
        ("BACKUP_FOLDER", "backups", "The backup folder must be an absolute path, such as /mnt/backup/restic."),
        ("BACKUP_FOLDER", "/mnt/backup/restic", ""),
        ("BACKUP_URL", "nas/restic", "A restic repository starts with its kind, such as sftp: or s3:."),
        ("BACKUP_URL", "s3:s3.amazonaws.com/bucket/restic", ""),
        ("PORTAINER_ADMIN_PASSWORD", "short", "Portainer needs at least 12 characters."),
        ("PORTAINER_ADMIN_PASSWORD", "portainer-pass-long", ""),
        ("SERVER_COUNTRIES", "", ""),
    ],
)
def test_each_answer_is_checked(settings, name, value, problem):
    assert settings.problem(name, value) == problem


def test_a_landing_page_port_in_use_is_refused_with_a_free_one_suggested(settings, ports_in_use):
    ports_in_use.update({80, 9000})
    assert settings.problem("HOMEPAGE_PORT", "9000") == "Port 9000 is in use on this machine; choose another, such as 8080."


def test_a_local_backup_folder_is_made_and_left_empty(settings, tmp_path):
    folder = tmp_path / "backups" / "restic"
    assert settings.backup_problem({"BACKUP_TYPE": "local", "BACKUP_FOLDER": str(folder)}) == ""
    assert folder.is_dir() and os.listdir(folder) == []


def test_a_local_backup_folder_that_cannot_be_made_is_explained(settings, tmp_path):
    if os.geteuid() == 0:
        pytest.skip("root can write into any folder")
    readonly = tmp_path / "readonly"
    readonly.mkdir()
    readonly.chmod(0o555)
    try:
        problem = settings.backup_problem({"BACKUP_TYPE": "local", "BACKUP_FOLDER": str(readonly / "backups")})
    finally:
        readonly.chmod(0o755)
    assert problem == f"{readonly}/backups cannot be created or written to by {pwd.getpwuid(os.geteuid()).pw_name}."


@pytest.mark.parametrize("code", [0, 10])
def test_b2_details_that_open_the_backups_or_find_none_yet_are_fine(settings, commands, code):
    commands.on(["restic", "-r", "b2:bucket:restic", "cat", "config"], done(returncode=code))
    assert settings.backup_problem(dict(B2)) == ""
    env = commands.env_of("restic")
    assert (env["B2_ACCOUNT_ID"], env["B2_ACCOUNT_KEY"], env["RESTIC_PASSWORD"]) == ("0031keyid", "K005key", "restic-typed")


def test_a_restic_password_that_does_not_open_the_b2_backups_is_explained(settings, commands):
    commands.on(["restic"], done(returncode=12, stderr="Fatal: wrong password or no key found\n"))
    assert settings.backup_problem(dict(B2)) == "The restic password does not open the backups already in b2:bucket:restic."


def test_b2_refusing_the_keys_is_explained_with_restics_last_line(settings, commands):
    commands.on(["restic"], done(returncode=1, stderr="Fatal: unable to open config file\nb2_authorize_account: 401: bad auth\n\n"))
    assert settings.backup_problem(dict(B2)) == "Backblaze B2 did not accept these details: b2_authorize_account: 401: bad auth"


def test_a_b2_check_that_hangs_is_stopped_and_reported(settings, commands):
    commands.on(["restic"], timed_out(stderr="Load(<config/0000000000>) returned error, retrying\n"))
    assert settings.backup_problem(dict(B2)) == "Backblaze B2 did not accept these details: Load(<config/0000000000>) returned error, retrying"


def test_without_restic_b2_details_are_not_checked(settings, commands, monkeypatch):
    monkeypatch.setattr(shutil, "which", lambda name, *args, **kwargs: None)
    assert settings.backup_problem(dict(B2)) == ""
    assert not commands.did("restic")


def test_another_kind_of_restic_repository_is_not_checked(settings, commands):
    assert settings.backup_problem({"BACKUP_TYPE": "other", "BACKUP_URL": "sftp:backup@nas:/srv/restic"}) == ""
    assert commands.ran == []


def test_the_sections_follow_the_fields_and_a_section_lists_only_the_fields_that_apply(settings):
    values = {"CONFIG_LOCATION": "local", "BACKUP_TYPE": "local"}
    assert settings.sections(values) == ["Installation", "Backup", "VPN", "Healthchecks (optional)", "App logins", "Subtitles", "Landing page"]
    assert [field.name for field in settings.fields_in("Backup", values)] == ["BACKUP_TYPE", "BACKUP_FOLDER", "RESTIC_PASSWORD"]
