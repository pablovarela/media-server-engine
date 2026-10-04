import os

import yaml

from engine import commands, installation, program

IMAGE_FILES = ("images.yml", "images.monitoring.yml", "compose.override.yml")
LISTING = ["docker", "image", "ls", "-a", "--digests", "--format", "{{.Repository}} {{.Tag}} {{.Digest}}"]


def untagged(name):
    tag = name.rfind(":")
    return name[:tag] if tag > name.rfind("/") else name


def pinned_references():
    pinned = set()
    for file_name in IMAGE_FILES:
        try:
            with open(os.path.join(installation.config_dir(), file_name)) as images:
                services = (yaml.safe_load(images) or {}).get("services") or {}
            declared = [service.get("image") for service in services.values() if isinstance(service, dict)]
        except (OSError, yaml.YAMLError, AttributeError):
            continue
        for image in declared:
            if isinstance(image, str) and "@sha256:" in image:
                name, digest = image.split("@", 1)
                pinned.add(f"{untagged(name)}@{digest}")
    return pinned


def outdated():
    pinned = pinned_references()
    repositories = {reference.split("@", 1)[0] for reference in pinned}
    found = []
    for line in commands.output(LISTING).splitlines():
        repository, tag, digest = line.split()
        if repository not in repositories or f"{repository}@{digest}" in pinned:
            continue
        found.append(f"{repository}@{digest}" if tag == "<none>" else f"{repository}:{tag}")
    return sorted(set(found))


def prune():
    for image in outdated():
        if commands.quiet(["docker", "image", "rm", image]) == 0:
            print(f"removed {image}")
        else:
            print(f"kept {image} (still in use)")


def main(argv):
    return program.run("prune-stack-images", lambda argv: prune(), argv)
