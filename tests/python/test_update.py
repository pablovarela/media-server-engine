import http.client
import json
import os
import pwd
import shutil
import time
import urllib.request
from types import SimpleNamespace
from unittest import mock

import pytest

from conftest import REPO, done, fresh_engine, real


class Restarted(Exception):
    pass


@pytest.fixture
def update(dirs, commands, urlopen, monkeypatch, tmp_path):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    for name in ("MEDIA_SERVER_PULLED", "MACHINE_ROLE", "COMPOSE_PROFILES", "TZ", "HOMEPAGE_PORT", "HOMEPAGE_ALLOWED_HOSTS"):
        os.environ.pop(name, None)
    os.environ.update(HOME=str(tmp_path / "home"), SYSTEMD_RUNTIME_DIR=str(tmp_path), MEDIA_SERVER_HOST="media.local")
    monkeypatch.setattr(shutil, "which", lambda name: f"/usr/bin/{name}")
    (dirs.config / "installation.env").write_text("INSTALLATION_NAME=testinst\n")
    (dirs.config / "engine.env").write_text("ENGINE_VERSION=v1.0.0\n")
    (dirs.engine / "installation").mkdir()
    shutil.copy(REPO / "installation" / "Makefile", dirs.engine / "installation" / "Makefile")
    engine, config = str(dirs.engine), str(dirs.config)
    mounts = {"services": {"sonarr": {"volumes": [
        {"type": "bind", "source": f"{dirs.data}/volumes/sonarr/data"},
        {"type": "bind", "source": f"{dirs.data}/media/tvshows"},
        {"type": "bind", "source": "/elsewhere"},
    ]}}}
    for words, outcome in (
        (["git", "-C", engine, "status"], done()),
        (["git", "-C", config, "status"], done()),
        (["git", "-C", config, "remote", "get-url", "origin"], done(stdout="git@github.com:someone/config.git\n")),
        (["git", "-C", config, "pull", "--ff-only"], done()),
        (["git", "-C", engine, "describe", "--tags", "--exact-match"], done(stdout="v1.0.0\n")),
        (["git", "-C", engine, "describe", "--tags", "--always"], done(stdout="v1.0.0\n")),
        (["git", "-C", engine, "remote", "get-url", "origin"], done(stdout="github-media-server-engine:someone/media-server-engine.git\n")),
        (["git", "-C", engine, "rev-parse", "HEAD"], done(stdout="abc123\n")),
        (["bootstrap.sh", "--pinned-tools"], done()),
        (["bash", "-c"], real()),
        (["sops", "decrypt", "vpn.sops.env"], done(stdout="OPENVPN_USER=u\n")),
        (["sops", "decrypt", "apps.sops.env"], done(stdout="SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nPORTAINER_ADMIN_PASSWORD=pw 1\n")),
        (["timedatectl"], done(stdout="Europe/London\n")),
        (["docker", "inspect", "homepage"], done(stdout="false\n")),
        (["docker", "compose", "config", "--format", "json"], done(stdout=json.dumps(mounts))),
        (["docker", "compose", "pull"], done()),
        (["docker", "compose", "up"], done()),
        (["docker", "compose", "ps", "-q", "gluetun"], done(stdout="gluetun-current\n")),
        (["docker", "inspect", "{{.HostConfig.NetworkMode}}"], done(stdout="container:gluetun-current\n")),
        (["wire_apps.py"], done()),
    ):
        commands.on(words, outcome)
    monkeypatch.setattr(os, "execv", mock.Mock(side_effect=Restarted))
    monkeypatch.setattr(time, "sleep", mock.Mock())
    module = fresh_engine("engine.update")
    checked = []
    monkeypatch.setattr(module.stack, "check", lambda: checked.append(len(commands.ran)))
    pruned = []
    monkeypatch.setattr(module.images, "prune", lambda: pruned.append(len(commands.ran)))
    synced = []
    monkeypatch.setattr(module.healthchecks, "sync", lambda checks, facts: synced.append(SimpleNamespace(checks=checks, facts=facts)))
    return SimpleNamespace(run=lambda *argv: module.main(list(argv)), module=module, synced=synced, execv=os.execv, checked=checked, pruned=pruned)


def config_status(commands, dirs, status):
    commands.on(["git", "-C", str(dirs.config), "status"], done(stdout=status))


def local_config(commands, dirs):
    commands.on(["git", "-C", str(dirs.config), "remote", "get-url", "origin"], done(stderr="error: No such remote 'origin'\n", returncode=2))


def pin_homepage(dirs):
    (dirs.config / "images.yml").write_text("services:\n  homepage:\n    image: ghcr.io/gethomepage/homepage:v2.4.0@sha256:abc\n")
    (dirs.engine / "homepage").mkdir()
    for page in (REPO / "homepage").glob("*.yaml"):
        shutil.copy(page, dirs.engine / "homepage")


def env_file(dirs):
    return (dirs.engine / ".env").read_text().splitlines()


def test_local_changes_in_the_config_stop_the_update_before_pulling(update, commands, dirs, capsys):
    config_status(commands, dirs, " M images.yml\n")
    assert update.run() == 1
    assert "not committed" in capsys.readouterr().err
    assert not commands.did("pull", "--ff-only")


def test_local_changes_to_a_local_only_config_are_shown_and_to_be_committed(update, commands, dirs, capsys):
    local_config(commands, dirs)
    config_status(commands, dirs, " M prowlarr.yml\n")
    assert update.run() == 1
    err = capsys.readouterr().err
    assert err == (
        "The config has changes that are not committed:\n M prowlarr.yml\n"
        f"See them with: git -C {dirs.config} diff\n"
        "Commit them, then run make update again:\n"
        f'  git -C {dirs.config} commit -am "<what changed>"\n'
    )


def test_local_changes_to_a_config_on_github_are_to_be_committed_and_pushed(update, commands, dirs, capsys):
    config_status(commands, dirs, " M prowlarr.yml\n")
    assert update.run() == 1
    assert f'  git -C {dirs.config} commit -am "<what changed>" && git -C {dirs.config} push\n' in capsys.readouterr().err


def test_an_untracked_file_in_the_config_such_as_a_compose_override_stops_the_update(update, commands, dirs, capsys):
    config_status(commands, dirs, "?? compose.override.yml\n")
    assert update.run() == 1
    assert "compose.override.yml" in capsys.readouterr().err
    assert not commands.did("docker", "compose", "up")


def test_a_local_only_config_is_used_as_it_is_without_pulling(update, commands, dirs):
    local_config(commands, dirs)
    assert update.run() == 0
    assert not commands.did("pull", "--ff-only")
    assert commands.did("docker", "compose", "up", "-d", "--remove-orphans")


def test_local_changes_in_the_engine_stop_the_update_before_pulling(update, commands, dirs, capsys):
    commands.on(["git", "-C", str(dirs.engine), "status"], done(stdout=" M scripts/engine/update.py\n"))
    assert update.run() == 1
    err = capsys.readouterr().err
    assert err == (
        " M scripts/engine/update.py\n"
        f"update: uncommitted changes in the engine at {dirs.engine}; an installation's engine is not edited, change the engine repository instead\n"
    )
    assert commands.ran[0].args == ["git", "-C", str(dirs.engine), "status", "--porcelain", "--untracked-files=no"]
    assert not commands.did("pull", "--ff-only")


def test_update_pulls_the_config_decrypts_and_brings_the_stack_up(update, commands, dirs):
    assert update.run() == 0
    assert commands.did("git", "-C", str(dirs.config), "pull", "--ff-only")
    assert commands.did("docker", "compose", "pull", "--quiet")
    assert commands.did("docker", "compose", "up", "-d", "--remove-orphans")
    assert (dirs.engine / ".secrets" / "vpn.env").read_text() == "OPENVPN_USER=u\n"


def test_wiring_step_images_are_pulled_and_their_mounts_created_but_up_leaves_them_to_wiring(update, commands):
    assert update.run() == 0
    assert commands.env_of("docker", "compose", "pull")["COMPOSE_PROFILES"] == "wiring"
    assert commands.env_of("docker", "compose", "config")["COMPOSE_PROFILES"] == "wiring"
    assert commands.env_of("docker", "compose", "up", "--remove-orphans")["COMPOSE_PROFILES"] == ""


def test_update_passes_the_installations_time_zone_to_compose(update, dirs):
    (dirs.config / "installation.env").write_text("INSTALLATION_NAME=testinst\nTZ=Europe/London\n")
    update.run()
    assert "TZ=Europe/London" in env_file(dirs)


def test_without_a_time_zone_compose_runs_in_utc(update, dirs):
    update.run()
    assert "TZ=Etc/UTC" in env_file(dirs)


def test_update_writes_the_docker_socket_group_id_for_compose(update, dirs):
    update.run()
    assert any(line.startswith("DOCKER_GID=") and line[len("DOCKER_GID="):].isdigit() for line in env_file(dirs))


def test_update_creates_the_bind_mount_directories_under_the_data_directory(update, dirs):
    assert update.run() == 0
    assert (dirs.data / "volumes" / "sonarr" / "data").is_dir()
    assert (dirs.data / "media" / "tvshows").is_dir()


def test_update_switches_the_engine_to_the_version_the_config_pins_then_runs_from_it(update, commands, dirs):
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--exact-match"], done(stdout="v0.9.0\n"))
    commands.on(["git", "-C", str(dirs.engine), "fetch"], done())
    commands.on(["git", "-C", str(dirs.engine), "rev-parse", "-q", "--verify", "refs/tags/v1.0.0"], done(stdout="abc123\n"))
    commands.on(["git", "-C", str(dirs.engine), "checkout", "-q", "--detach", "v1.0.0"], done())
    with pytest.raises(Restarted):
        update.run("--verbose")
    program = str(REPO / "scripts" / "engine-run")
    update.execv.assert_called_once_with(program, [program, "update", "--verbose"])
    assert os.access(program, os.X_OK)
    assert os.environ["MEDIA_SERVER_PULLED"] == "1"
    assert commands.count("pull", "--ff-only") == 1




def test_once_restarted_the_update_does_not_pull_or_switch_again(update, commands):
    os.environ["MEDIA_SERVER_PULLED"] = "1"
    assert update.run() == 0
    assert not commands.did("pull", "--ff-only")
    assert not commands.did("describe", "--exact-match")


def test_update_stays_on_the_current_engine_when_the_pinned_version_does_not_exist(update, commands, dirs, capsys):
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--exact-match"], done(stdout="v0.9.0\n"))
    commands.on(["git", "-C", str(dirs.engine), "fetch"], done())
    commands.on(["git", "-C", str(dirs.engine), "rev-parse"], done(returncode=1))
    assert update.run() == 1
    assert capsys.readouterr().err == "update: engine version v1.0.0 not found; staying on v0.9.0\n"
    assert not commands.did("checkout")
    assert not commands.did("docker", "compose", "up")


def test_update_leaves_the_engine_alone_when_it_already_runs_the_pinned_version(update, commands):
    assert update.run() == 0
    assert not commands.did("checkout")
    update.execv.assert_not_called()


def test_engine_version_local_runs_the_checked_out_engine_without_switching(update, commands, dirs):
    (dirs.config / "engine.env").write_text("ENGINE_VERSION=local\n")
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--exact-match"], done(stdout="v0.9.0\n"))
    assert update.run() == 0
    assert not commands.did("checkout")


def test_update_installs_the_tools_the_engine_pins_before_bringing_the_stack_up(update, commands):
    assert update.run() == 0
    assert commands.index("bootstrap.sh", "--pinned-tools") < commands.index("docker", "compose", "pull")


def test_update_stops_before_bringing_the_stack_up_when_the_merged_compose_is_unsafe(update, commands, monkeypatch):
    def unsafe():
        raise update.module.commands.Stop("the merged compose files are not safe to deploy")

    monkeypatch.setattr(update.module.stack, "check", unsafe)
    assert update.run() == 1
    assert not commands.did("docker", "compose", "up")


def test_update_wires_the_apps_after_the_stack_is_up_and_prunes_last(update, commands):
    assert update.run() == 0
    assert commands.index("docker", "compose", "up", "--remove-orphans") < commands.index("wire_apps.py")
    assert update.pruned == [len(commands.ran)]


def test_update_recreates_gluetun_dependents_attached_to_an_old_gluetun(update, commands):
    commands.on(["docker", "inspect", "{{.HostConfig.NetworkMode}}"], done(stdout="container:gluetun-old\n"))
    assert update.run() == 0
    assert commands.did("docker", "compose", "up", "-d", "--force-recreate", "--no-deps", "prowlarr", "flaresolverr", "deluge")


def test_update_leaves_gluetun_dependents_alone_when_attached_to_the_current_gluetun(update, commands):
    assert update.run() == 0
    assert not commands.did("--force-recreate")


def test_when_bringing_the_stack_up_fails_dependents_are_reattached_and_nothing_is_wired_or_pruned(update, commands):
    commands.on(["docker", "compose", "up", "-d", "--remove-orphans"], done(returncode=17))
    commands.on(["docker", "inspect", "{{.HostConfig.NetworkMode}}"], done(stdout="container:gluetun-old\n"))
    assert update.run() == 17
    assert commands.did("--force-recreate", "--no-deps", "prowlarr", "flaresolverr", "deluge")
    assert not commands.did("wire_apps.py")
    assert not update.pruned


def test_update_waits_for_a_running_backup_and_gives_up_with_a_message(update, commands, monkeypatch, capsys):
    monkeypatch.setattr(update.module.backups, "running_backup_pid", lambda: "4242")
    os.environ.update(UPDATE_BACKUP_WAIT_SECONDS="0")
    assert update.run() == 1
    assert capsys.readouterr().err == "update: a backup is still running; run make update again once it has finished\n"
    assert not commands.did("docker", "compose", "up")


def test_update_carries_on_once_the_running_backup_finishes(update, monkeypatch, capsys):
    pids = iter(["4242", "4242", ""])
    monkeypatch.setattr(update.module.backups, "running_backup_pid", lambda: next(pids))
    assert update.run() == 0
    assert capsys.readouterr().out.count("Waiting for the running backup to finish...") == 1
    time.sleep.assert_called_with(10)


def test_update_writes_the_installations_makefile(update, dirs):
    assert update.run() == 0
    assert (dirs.engine.parent / "Makefile").is_file()


def test_the_landing_page_answers_to_this_machines_name_plus_any_names_the_config_adds(update, dirs):
    update.run()
    assert "HOMEPAGE_PORT=80" in env_file(dirs)
    assert "HOMEPAGE_ALLOWED_HOSTS=media.local,localhost,127.0.0.1" in env_file(dirs)
    with open(dirs.config / "installation.env", "a") as installation:
        installation.write("HOMEPAGE_ALLOWED_HOSTS=media.tailnet.ts.net\n")
    update.run()
    assert "HOMEPAGE_ALLOWED_HOSTS=media.local,localhost,127.0.0.1,media.tailnet.ts.net" in env_file(dirs)


def test_the_landing_page_can_use_another_port_and_answers_to_its_address_with_that_port(update, dirs):
    with open(dirs.config / "installation.env", "a") as installation:
        installation.write("HOMEPAGE_PORT=8080\n")
    update.run()
    assert "HOMEPAGE_PORT=8080" in env_file(dirs)
    assert "HOMEPAGE_ALLOWED_HOSTS=media.local:8080,localhost:8080,127.0.0.1:8080" in env_file(dirs)


def test_update_renders_the_landing_page_and_its_secrets_before_bringing_the_stack_up(update, commands, dirs):
    pin_homepage(dirs)
    assert update.run() == 0
    assert 'title: "testinst"' in (dirs.engine / ".homepage" / "settings.yaml").read_text()
    assert "Sonarr:" in (dirs.engine / ".homepage" / "services.yaml").read_text()
    assert "HOMEPAGE_VAR_SONARR_KEY=s1" in (dirs.engine / ".secrets" / "homepage.env").read_text().splitlines()
    assert os.stat(dirs.engine / ".secrets" / "homepage.env").st_mode & 0o777 == 0o600


def test_a_key_the_wiring_creates_reaches_the_landing_page_which_restarts_only_then(update, commands, dirs):
    pin_homepage(dirs)

    def wire():
        (dirs.data / "volumes" / ".wiring").mkdir(parents=True, exist_ok=True)
        (dirs.data / "volumes" / ".wiring" / "jellyfin.key").write_text("new-jellyfin-key\n")

    commands.on(["wire_apps.py"], done(then=wire))
    assert update.run() == 0
    assert "HOMEPAGE_VAR_JELLYFIN_KEY=new-jellyfin-key" in (dirs.engine / ".secrets" / "homepage.env").read_text().splitlines()
    assert commands.index("docker", "compose", "up", "-d", "homepage") > commands.index("wire_apps.py")
    commands.ran.clear()
    assert update.run() == 0
    assert not commands.did("docker", "compose", "up", "-d", "homepage")


def test_a_failed_wiring_still_refreshes_the_landing_pages_keys_then_fails_the_update(update, commands, dirs):
    pin_homepage(dirs)

    def wire():
        (dirs.data / "volumes" / ".wiring").mkdir(parents=True, exist_ok=True)
        (dirs.data / "volumes" / ".wiring" / "jellyfin.key").write_text("k2\n")

    commands.on(["wire_apps.py"], done(returncode=1, then=wire))
    assert update.run() == 1
    assert "HOMEPAGE_VAR_JELLYFIN_KEY=k2" in (dirs.engine / ".secrets" / "homepage.env").read_text().splitlines()
    assert not update.pruned


def test_the_landing_pages_variables_stay_out_of_the_wirings_environment(update, commands):
    seen = {}
    commands.on(["wire_apps.py"], done(then=lambda: seen.update(host=os.environ.get("HOMEPAGE_HOST"))))
    assert update.run() == 0
    assert seen == {"host": None}


def test_the_landing_page_links_the_engine_version_to_its_release_on_github(update, dirs):
    pin_homepage(dirs)
    assert update.run() == 0
    assert "href: https://github.com/someone/media-server-engine/releases/tag/v1.0.0" in (dirs.engine / ".homepage" / "widgets.yaml").read_text()


def test_between_releases_the_landing_page_links_the_engine_version_to_its_commit(update, commands, dirs):
    pin_homepage(dirs)
    commands.on(["git", "-C", str(dirs.engine), "describe", "--tags", "--always"], done(stdout="v1.0.0-3-gabc1234\n"))
    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="https://github.com/someone/media-server-engine.git\n"))
    assert update.run() == 0
    assert "href: https://github.com/someone/media-server-engine/commit/abc123" in (dirs.engine / ".homepage" / "widgets.yaml").read_text()


def test_a_running_landing_page_is_asked_to_reload_its_page(update, commands, dirs, http):
    commands.on(["docker", "inspect", "homepage"], done(stdout="true\n"))
    http.on("GET", "http://localhost:80/api/revalidate", {})
    assert update.run() == 0
    assert not commands.did("docker", "restart", "homepage")


def test_a_running_landing_page_restarts_when_its_images_change(update, commands, dirs, urlopen):
    commands.on(["docker", "inspect", "homepage"], done(stdout="true\n"))
    commands.on(["docker", "restart", "homepage"], done())
    (dirs.config / "homepage" / "images").mkdir(parents=True)
    (dirs.config / "homepage" / "images" / "background.jpg").write_text("new")
    assert update.run() == 0
    assert commands.did("docker", "restart", "homepage")


def refused(error):
    return done(stderr=f"Error response from daemon: {error}\n", returncode=1)


def test_a_pull_refused_as_too_many_requests_is_tried_again_and_the_update_carries_on(update, commands, capsys):
    os.environ["UPDATE_PULL_RETRY_SECONDS"] = "0"
    toomany = refused("toomanyrequests: retry-after: 896.394µs, allowed: 44000/minute")
    commands.on(["docker", "compose", "pull"], toomany, toomany, done())
    assert update.run() == 0
    assert commands.count("docker", "compose", "pull") == 3
    output = capsys.readouterr()
    assert "toomanyrequests" in output.err
    assert "A registry is limiting requests; trying the pull again in 0 seconds..." in output.out
    assert commands.did("wire_apps.py")


def test_retries_wait_longer_each_time(update, commands):
    toomany = refused("toomanyrequests")
    commands.on(["docker", "compose", "pull"], toomany, toomany, done())
    assert update.run() == 0
    assert [call.args[0] for call in time.sleep.call_args_list] == [30, 60]


def test_update_gives_up_when_a_registry_keeps_refusing_pulls_as_too_many_requests(update, commands, capsys):
    os.environ.update(UPDATE_PULL_ATTEMPTS="3", UPDATE_PULL_RETRY_SECONDS="0")
    commands.on(["docker", "compose", "pull"], refused("toomanyrequests: retry-after: 1s"))
    assert update.run() == 1
    assert commands.count("docker", "compose", "pull") == 3
    assert capsys.readouterr().err.endswith("update: a registry kept refusing pulls as too many requests; run make update again later\n")
    assert not commands.did("docker", "compose", "up", "-d", "--remove-orphans")


def test_a_pull_that_fails_for_another_reason_is_not_tried_again(update, commands, capsys):
    commands.on(["docker", "compose", "pull"], refused("manifest unknown"))
    assert update.run() == 1
    assert commands.count("docker", "compose", "pull") == 1
    assert "manifest unknown" in capsys.readouterr().err
    assert not commands.did("docker", "compose", "up", "-d", "--remove-orphans")


def test_a_pull_refused_with_a_bare_429_status_is_tried_again(update, commands):
    os.environ["UPDATE_PULL_RETRY_SECONDS"] = "0"
    commands.on(["docker", "compose", "pull"], refused("unexpected status from HEAD request to https://ghcr.io/v2/x/manifests/1: 429 Too Many Requests"), done())
    assert update.run() == 0
    assert commands.count("docker", "compose", "pull") == 2


def test_the_main_sets_up_its_backup_verify_and_update_checks_before_pulling_images(update, commands, dirs):
    (dirs.data / ".backup-main").touch()
    seen = []
    update.module.healthchecks.sync = lambda checks, facts: seen.append((checks, len(commands.ran)))
    assert update.run() == 0
    checks, ran_before = seen[0]
    assert checks == [("backup", "testinst-backup"), ("verify", "testinst-verify"), ("update", "testinst-update")]
    assert ran_before <= commands.index("docker", "compose", "pull")


def test_a_machine_that_is_not_the_main_sets_up_only_its_own_update_check(update, monkeypatch):
    monkeypatch.setattr(update.module.installation, "short_hostname", lambda: "laptop")
    assert update.run() == 0
    assert update.synced[0].checks == [("update", "testinst-update-laptop")]


def test_an_update_run_as_the_main_sets_up_the_mains_checks_before_the_machine_is_claimed(update):
    os.environ["MACHINE_ROLE"] = "main"
    assert update.run() == 0
    assert update.synced[0].checks == [("backup", "testinst-backup"), ("verify", "testinst-verify"), ("update", "testinst-update")]


def test_the_checks_tell_how_to_reach_this_machine_and_its_installation(update, dirs):
    with open(dirs.config / "installation.env", "a") as installation:
        installation.write("MEDIA_SERVER_HOST=media.example\nRESTIC_REPOSITORY=b2:bucket:testinst\n")
    assert update.run() == 0
    facts = update.synced[0].facts
    assert facts.name == "testinst"
    assert facts.tz == "Europe/London"
    assert facts.repository == "b2:bucket:testinst"
    assert facts.ssh == f"{pwd.getpwuid(os.geteuid()).pw_name}@media.example"
    assert facts.directory == os.path.realpath(dirs.engine.parent)


def test_an_installation_under_the_home_directory_is_shown_with_a_tilde(update, dirs):
    os.environ["HOME"] = str(dirs.engine.parent.parent)
    assert update.run() == 0
    assert update.synced[0].facts.directory == f"~/{dirs.engine.parent.name}"


def test_failing_to_set_up_the_checks_does_not_stop_the_update(update, commands, capsys):
    def broken(checks, facts):
        raise RuntimeError("boom")

    update.module.healthchecks.sync = broken
    assert update.run() == 0
    assert "could not set up the healthchecks.io checks (boom); carrying on" in capsys.readouterr().err
    assert update.pruned


def test_a_machine_without_systemd_timers_sets_up_no_checks(update, dirs, monkeypatch):
    monkeypatch.setattr(shutil, "which", lambda name: None)
    (dirs.data / ".backup-main").touch()
    assert update.run() == 0
    assert update.synced == []


def test_a_running_landing_page_that_does_not_answer_the_reload_does_not_stop_the_update(update, commands):
    commands.on(["docker", "inspect", "homepage"], done(stdout="true\n"))
    assert update.run() == 0
    assert update.pruned


def test_a_missing_engine_pin_stops_the_update_with_one_line(update, dirs, capsys):
    (dirs.config / "engine.env").unlink()
    assert update.run() == 1
    err = capsys.readouterr().err
    assert err.startswith("update: ") and "engine.env" in err
    assert err.count("\n") == 1


def test_an_interrupted_update_ends_with_the_status_the_shell_gives(update, monkeypatch):
    def interrupt():
        raise KeyboardInterrupt

    monkeypatch.setattr(update.module.stack, "check", interrupt)
    assert update.run() == 130


def test_when_compose_cannot_say_which_gluetun_runs_every_dependent_is_reattached(update, commands):
    commands.on(["docker", "compose", "ps", "-q", "gluetun"], done(returncode=1))
    assert update.run() == 0
    assert commands.did("--force-recreate", "--no-deps", "prowlarr", "flaresolverr", "deluge")


def test_a_data_directory_set_in_the_installation_is_where_the_landing_page_finds_its_keys(update, dirs, tmp_path):
    elsewhere = tmp_path / "elsewhere"
    (elsewhere / "volumes" / ".wiring").mkdir(parents=True)
    (elsewhere / "volumes" / ".wiring" / "jellyfin.key").write_text("elsewhere-key\n")
    with open(dirs.config / "installation.env", "a") as installation:
        installation.write(f"DATA_DIR={elsewhere}\n")
    assert update.run() == 0
    assert "HOMEPAGE_VAR_JELLYFIN_KEY=elsewhere-key" in (dirs.engine / ".secrets" / "homepage.env").read_text().splitlines()


def test_a_garbled_answer_to_the_reload_does_not_stop_the_update(update, commands, monkeypatch):
    commands.on(["docker", "inspect", "homepage"], done(stdout="true\n"))

    def garbled(request, timeout=None):
        raise http.client.BadStatusLine("garbage")

    monkeypatch.setattr(urllib.request, "urlopen", garbled)
    assert update.run() == 0


def test_the_stack_is_checked_before_images_are_pulled(update, commands):
    assert update.run() == 0
    assert update.checked and update.checked[0] <= commands.index("docker", "compose", "pull")
