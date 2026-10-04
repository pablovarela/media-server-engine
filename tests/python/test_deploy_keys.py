import os
import time
from unittest import mock

import pytest

from conftest import done, fresh_engine

ACCEPTED = done(stderr="Hi someone/repo! You have successfully authenticated, but GitHub does not provide shell access.\n", returncode=1)
REFUSED = done(stderr="git@github.com: Permission denied (publickey).\n", returncode=255)
WANTED = ["someone/media-server-engine", "someone/media-server-config-testinst:write"]


def keygen(commands):
    def write():
        args = commands.ran[-1].args
        path = args[args.index("-f") + 1]
        with open(path, "w") as private:
            private.write("private\n")
        with open(path + ".pub", "w") as public:
            public.write("ssh-ed25519 AAAAtest deploy\n")

    commands.on(["ssh-keygen"], done(then=write))


@pytest.fixture
def keys(dirs, commands, tmp_path, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ.update(HOME=str(tmp_path / "home"), DEPLOY_KEY_POLL_SECONDS="0")
    (tmp_path / "home").mkdir()
    keygen(commands)
    commands.on(["gh", "auth", "status"], done(returncode=1))
    commands.on(["gh", "repo", "deploy-key", "add"])
    commands.on(["ssh", "-T"], REFUSED, ACCEPTED)
    monkeypatch.setattr(time, "sleep", mock.Mock())
    module = fresh_engine("engine.deploy_keys")
    monkeypatch.setattr(module.installation, "short_hostname", lambda: "box")
    return module


def ssh_dir(tmp_path):
    return tmp_path / "home" / ".ssh"


def test_a_key_and_an_ssh_alias_are_created_for_each_repository(keys, commands, tmp_path):
    keys.deploy(*WANTED)
    ssh = ssh_dir(tmp_path)
    assert (ssh / "media-server-engine-deploy").exists() and (ssh / "media-server-config-testinst-deploy").exists()
    assert commands.did("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(ssh / "media-server-engine-deploy"), "-C", "media-server-engine on box")
    config = (ssh / "config").read_text()
    assert (
        "Host github-media-server-engine\n  HostName github.com\n  User git\n"
        f"  IdentityFile {ssh}/media-server-engine-deploy\n  IdentitiesOnly yes\n  StrictHostKeyChecking accept-new\n"
    ) in config
    assert oct(os.stat(ssh).st_mode & 0o777) == "0o700"
    assert oct(os.stat(ssh / "config").st_mode & 0o777) == "0o600"


def test_running_again_reuses_the_keys_and_the_aliases(keys, commands, tmp_path):
    commands.on(["ssh", "-T"], ACCEPTED)
    keys.deploy(*WANTED)
    commands.ran.clear()
    keys.deploy(*WANTED)
    assert not commands.did("ssh-keygen")
    assert (ssh_dir(tmp_path) / "config").read_text().count("Host github-media-server-engine\n") == 1


def test_a_key_github_already_accepts_is_not_added_again(keys, commands, capsys):
    commands.on(["ssh", "-T"], ACCEPTED)
    keys.deploy(*WANTED)
    assert not commands.did("gh", "repo", "deploy-key")
    assert capsys.readouterr().out == ""


def test_without_gh_the_key_and_the_github_page_to_add_it_are_shown(keys, commands, capsys):
    keys.deploy("someone/media-server-engine")
    assert capsys.readouterr().out == (
        "Add this read-only deploy key to someone/media-server-engine:\n"
        "  ssh-ed25519 AAAAtest deploy\n"
        "  https://github.com/someone/media-server-engine/settings/keys/new\n"
        "Waiting for GitHub to accept the key for media-server-engine (Ctrl-C to stop)...\n"
        "GitHub accepts the key for media-server-engine.\n"
    )
    assert not commands.did("gh", "repo", "deploy-key")


def test_without_gh_the_configs_key_is_shown_with_the_write_access_it_needs(keys, capsys):
    keys.deploy("someone/media-server-config-testinst:write")
    assert capsys.readouterr().out.startswith("Add this deploy key to someone/media-server-config-testinst, with Allow write access ticked:\n")


def test_with_gh_logged_in_the_keys_are_added_for_you_read_only_for_the_engine(keys, commands, tmp_path):
    commands.on(["gh", "auth", "status"])
    commands.on(["ssh", "-T"], REFUSED, ACCEPTED, REFUSED, ACCEPTED)
    keys.deploy(*WANTED)
    ssh = ssh_dir(tmp_path)
    added = [command.args for command in commands.ran if command.args[:4] == ["gh", "repo", "deploy-key", "add"]]
    assert added == [
        ["gh", "repo", "deploy-key", "add", f"{ssh}/media-server-engine-deploy.pub", "--repo", "someone/media-server-engine", "--title", "box"],
        ["gh", "repo", "deploy-key", "add", f"{ssh}/media-server-config-testinst-deploy.pub", "--repo", "someone/media-server-config-testinst", "--title", "box", "--allow-write"],
    ]


def test_it_waits_until_github_accepts_the_key(keys, commands):
    commands.on(["ssh", "-T"], REFUSED, REFUSED, REFUSED, ACCEPTED)
    keys.deploy("someone/media-server-engine")
    assert commands.count("ssh", "-T", "-o", "BatchMode=yes", "github-media-server-engine") == 4
    assert [call.args[0] for call in time.sleep.call_args_list] == [0, 0]
