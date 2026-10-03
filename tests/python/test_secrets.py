import os
import re
import stat

import pytest

from conftest import done, fresh_engine

APPS = "SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nPORTAINER_ADMIN_PASSWORD=pw 1\n"


@pytest.fixture
def secrets(dirs, commands):
    commands.on(["sops", "decrypt", "--output-type", "dotenv", "vpn.sops.env"], done(stdout="OPENVPN_USER=u\n"))
    commands.on(["sops", "decrypt", "--output-type", "dotenv", "apps.sops.env"], done(stdout=APPS))
    commands.on(["sops", "decrypt", "--output-type", "dotenv", "healthchecks.sops.env"], done(stdout="HEALTHCHECKS_PING_KEY=ping\n"))
    return fresh_engine("engine.secrets")


def mode(path):
    return stat.S_IMODE(os.stat(path).st_mode)


def secret(dirs, name):
    return (dirs.engine / ".secrets" / name).read_text()


def test_the_vpn_and_app_secrets_are_decrypted_for_the_stack(secrets, dirs):
    secrets.decrypt_all()
    assert secret(dirs, "vpn.env") == "OPENVPN_USER=u\n"
    assert secret(dirs, "apps.env") == APPS


def test_decrypted_secrets_and_their_directories_are_the_owners_alone(secrets, dirs):
    secrets.decrypt_all()
    for name in ("vpn.env", "apps.env", "healthchecks.env", "gluetun.env", "configarr/secrets.yml", "sonarr.env", "radarr.env", "prowlarr.env", "portainer_admin"):
        assert mode(dirs.engine / ".secrets" / name) == 0o600, name
    assert mode(dirs.engine / ".secrets") == 0o700
    assert mode(dirs.engine / ".secrets" / "configarr") == 0o700


def test_a_secret_file_left_readable_is_made_the_owners_alone(secrets, dirs):
    (dirs.engine / ".secrets").mkdir()
    (dirs.engine / ".secrets" / "vpn.env").write_text("old")
    os.chmod(dirs.engine / ".secrets" / "vpn.env", 0o644)
    secrets.decrypt_all()
    assert mode(dirs.engine / ".secrets" / "vpn.env") == 0o600


def test_without_healthchecks_secrets_the_file_is_empty(secrets, dirs, commands):
    secrets.decrypt_all()
    assert secret(dirs, "healthchecks.env") == ""
    assert not commands.did("sops", "decrypt", "healthchecks.sops.env")


def test_healthchecks_secrets_are_decrypted_when_the_config_has_them(secrets, dirs):
    (dirs.config / "secrets").mkdir()
    (dirs.config / "secrets" / "healthchecks.sops.env").write_text("ENC")
    secrets.decrypt_all()
    assert secret(dirs, "healthchecks.env") == "HEALTHCHECKS_PING_KEY=ping\n"


def test_each_arr_gets_only_its_own_api_key_named_as_the_app_reads_it(secrets, dirs):
    secrets.decrypt_all()
    assert secret(dirs, "sonarr.env") == "SONARR__AUTH__APIKEY=s1\n"
    assert secret(dirs, "radarr.env") == "RADARR__AUTH__APIKEY=r1\n"
    assert secret(dirs, "prowlarr.env") == "PROWLARR__AUTH__APIKEY=p1\n"


def test_the_portainer_admin_password_file_holds_exactly_the_password(secrets, dirs):
    secrets.decrypt_all()
    assert secret(dirs, "portainer_admin") == "pw 1"


def test_configarr_gets_only_the_secrets_its_config_refers_to(secrets, dirs):
    (dirs.config / "configarr").mkdir()
    (dirs.config / "configarr" / "config.yml").write_text(
        "api_key: !secret SONARR_API_KEY\nother: !secret RADARR_API_KEY\nagain: !secret SONARR_API_KEY\n"
    )
    secrets.decrypt_all()
    assert secret(dirs, "configarr/secrets.yml") == 'SONARR_API_KEY: "s1"\nRADARR_API_KEY: "r1"\n'


def test_without_a_configarr_config_configarr_gets_no_secrets(secrets, dirs):
    secrets.decrypt_all()
    assert secret(dirs, "configarr/secrets.yml") == ""


def test_gluetuns_control_server_gets_a_key_that_is_made_once_and_kept(secrets, dirs):
    secrets.decrypt_all()
    key_file = dirs.data / "volumes" / ".wiring" / "gluetun-control.key"
    key = key_file.read_text().strip()
    assert re.fullmatch(r"[0-9a-f]{32}", key)
    assert mode(key_file) == 0o600
    assert secret(dirs, "gluetun.env") == f'HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE={{"auth":"apikey","apikey":"{key}"}}\n'
    secrets.decrypt_all()
    assert key_file.read_text().strip() == key


def test_a_failed_decryption_stops_with_the_status_of_sops(secrets, commands):
    commands.on(["sops", "decrypt", "--output-type", "dotenv", "vpn.sops.env"], done(returncode=128))
    with pytest.raises(secrets.commands.CommandFailed) as failed:
        secrets.decrypt_all()
    assert failed.value.returncode == 128


def test_the_folders_made_for_the_gluetun_key_are_the_owners_alone(secrets, dirs):
    secrets.decrypt_all()
    assert mode(dirs.data / "volumes") == 0o700
    assert mode(dirs.data / "volumes" / ".wiring") == 0o700


def test_secret_files_are_rewritten_in_place_so_a_running_container_sees_the_new_content(secrets, dirs):
    secrets.decrypt_all()
    inode = os.stat(dirs.engine / ".secrets" / "portainer_admin").st_ino
    secrets.decrypt_all()
    assert os.stat(dirs.engine / ".secrets" / "portainer_admin").st_ino == inode
