import os
import time

from engine import commands, installation

ALIAS = "Host github-{repo}\n  HostName github.com\n  User git\n  IdentityFile {key}\n  IdentitiesOnly yes\n  StrictHostKeyChecking accept-new\n"


def ensure_key(ssh, repo):
    key = os.path.join(ssh, f"{repo}-deploy")
    if not os.path.isfile(key):
        commands.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key, "-C", f"{repo} on {installation.short_hostname()}"])
    return key


def ensure_alias(ssh, repo, key):
    config = os.path.join(ssh, "config")
    open(config, "a").close()
    os.chmod(config, 0o600)
    with open(config) as current:
        if f"Host github-{repo}" in current.read().splitlines():
            return
    with open(config, "a") as aliases:
        aliases.write(ALIAS.format(repo=repo, key=key))


def accepted(repo):
    _, stdout, stderr = commands.captured(["ssh", "-T", "-o", "BatchMode=yes", f"github-{repo}"])
    return "successfully authenticated" in stdout + stderr


def add(owner_repo, repo, key, write):
    if commands.quiet(["gh", "auth", "status"]) == 0:
        commands.run(["gh", "repo", "deploy-key", "add", f"{key}.pub", "--repo", owner_repo, "--title", installation.short_hostname(), *(["--allow-write"] if write else [])])
    else:
        if write:
            print(f"Add this deploy key to {owner_repo}, with Allow write access ticked:")
        else:
            print(f"Add this read-only deploy key to {owner_repo}:")
        with open(f"{key}.pub") as public:
            print(f"  {public.read().strip()}")
        print(f"  https://github.com/{owner_repo}/settings/keys/new")
    print(f"Waiting for GitHub to accept the key for {repo} (Ctrl-C to stop)...")
    while not accepted(repo):
        time.sleep(float(os.environ.get("DEPLOY_KEY_POLL_SECONDS") or 5))
    print(f"GitHub accepts the key for {repo}.")


def deploy(*wanted):
    ssh = os.path.join(os.environ["HOME"], ".ssh")
    os.makedirs(ssh, exist_ok=True)
    os.chmod(ssh, 0o700)
    for item in wanted:
        write = item.endswith(":write")
        owner_repo = item[: -len(":write")] if write else item
        repo = owner_repo.split("/", 1)[1]
        key = ensure_key(ssh, repo)
        ensure_alias(ssh, repo, key)
        if not accepted(repo):
            add(owner_repo, repo, key, write)
