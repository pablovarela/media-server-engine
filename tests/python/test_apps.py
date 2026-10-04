import os
import platform
import socket

import pytest

from conftest import REPO, done, fresh_engine

APPS = [("Jellyfin", 8096), ("Seerr", 5055), ("Sonarr", 8989), ("Radarr", 7878), ("Prowlarr", 9696), ("Bazarr", 6767), ("Deluge", 8112), ("Maintainerr", 6246), ("Portainer", 9000)]
SECRETS = "JELLYFIN_ADMIN_PASSWORD=jelly pass\nDELUGE_WEB_PASSWORD=deluge-pass\nPORTAINER_ADMIN_PASSWORD=portainer-pass\nSONARR_API_KEY=k\n"


@pytest.fixture
def apps(installed, commands, monkeypatch):
    (installed.config / "installation.env").write_text("INSTALLATION_NAME=testinst\nJELLYFIN_ADMIN_USER=admin\n")
    monkeypatch.setattr(platform, "system", lambda: "Linux")
    monkeypatch.setattr(socket, "gethostname", lambda: "homeserver")
    commands.on(["sops", "decrypt", "--output-type", "dotenv", "apps.sops.env"], done(stdout=SECRETS))
    return fresh_engine("engine.apps")


def pin_homepage(installed):
    (installed.config / "images.yml").write_text("services:\n  homepage:\n    image: ghcr.io/gethomepage/homepage:v2.4.0@sha256:abc\n")


def test_urls_lists_every_app_at_this_machines_local_network_name(apps, capsys):
    assert apps.urls_main([]) == 0
    assert capsys.readouterr().out == "".join(f"{name:<12} http://homeserver.local:{port}\n" for name, port in APPS)


def test_every_listed_port_is_published_by_the_engines_compose_file(apps, capsys):
    apps.urls_main([])
    compose = (REPO / "docker-compose.yml").read_text()
    for line in capsys.readouterr().out.splitlines():
        assert f'"{line.rsplit(":", 1)[1]}:' in compose, line


def test_the_host_name_can_be_overridden(apps, installed, capsys):
    with open(installed.config / "installation.env", "a") as installation:
        installation.write("MEDIA_SERVER_HOST=192.168.1.20\n")
    apps.urls_main([])
    assert "http://192.168.1.20:8096\n" in capsys.readouterr().out


def test_on_macos_the_addresses_use_the_name_the_mac_announces(apps, commands, monkeypatch, capsys):
    monkeypatch.setattr(platform, "system", lambda: "Darwin")
    commands.on(["scutil", "--get", "LocalHostName"], done(stdout="bonjour-name\n"))
    apps.urls_main([])
    assert "http://bonjour-name.local:8096\n" in capsys.readouterr().out


def test_the_landing_page_is_listed_first_when_the_config_pins_it(apps, installed, capsys):
    pin_homepage(installed)
    apps.urls_main([])
    assert capsys.readouterr().out.splitlines()[0] == "Home         http://homeserver.local"


def test_the_landing_pages_address_includes_its_port_when_it_is_not_80(apps, installed, capsys):
    pin_homepage(installed)
    with open(installed.config / "installation.env", "a") as installation:
        installation.write("HOMEPAGE_PORT=8080\n")
    apps.urls_main([])
    assert capsys.readouterr().out.splitlines()[0] == "Home         http://homeserver.local:8080"


def test_without_the_landing_page_pinned_it_is_not_listed(apps, capsys):
    apps.urls_main([])
    assert not capsys.readouterr().out.startswith("Home")


def test_logins_show_each_apps_user_and_password_from_the_secrets_and_no_api_keys(apps, commands, capsys):
    assert apps.logins_main([]) == 0
    assert capsys.readouterr().out == (
        "App          User       Password\n"
        "Jellyfin     admin      jelly pass\n"
        "Seerr        sign in with the Jellyfin account\n"
        "Deluge                  deluge-pass\n"
        "Portainer    admin      portainer-pass\n"
        "Sonarr       no login on the local network\n"
        "Radarr       no login on the local network\n"
        "Prowlarr     no login on the local network\n"
    )
    assert commands.count("sops", "decrypt") == 1
