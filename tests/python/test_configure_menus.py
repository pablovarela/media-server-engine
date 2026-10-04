import os
import shutil

import pytest

from conftest import done, fresh_engine, guided_answers, whiptail, whiptail_screens

KEYS_HINT = "Arrows move, Enter chooses, Tab reaches the buttons, Esc goes back."


@pytest.fixture
def menus(dirs, commands, tmp_path, monkeypatch):
    monkeypatch.setattr(os, "environ", dict(os.environ))
    os.environ["HOME"] = str(tmp_path / "home")
    monkeypatch.chdir(dirs.config)
    (dirs.config / "apps.yml").write_text("bazarr:\n  languages: [en]\n")
    commands.on(["git", "-C", str(dirs.engine), "remote", "get-url", "origin"], done(stdout="git@github.com:someone/media-server-engine.git\n"))
    commands.on(["restic"], done(returncode=10))
    monkeypatch.setattr(shutil, "which", lambda name, *args, **kwargs: f"/usr/bin/{name}")
    module = fresh_engine("engine.configure_menus")
    monkeypatch.setattr(module.settings.ports, "port_in_use", lambda port: False)
    return module


def new():
    return {"INSTALLATION_NAME": "testinst"}


def existing(folder):
    return {
        "INSTALLATION_NAME": "testinst", "TZ": "Europe/London", "CONFIG_LOCATION": "local", "JELLYFIN_ADMIN_USER": "admin",
        "RESTIC_REPOSITORY": str(folder), "BACKUP_TYPE": "local", "BACKUP_FOLDER": str(folder), "RESTIC_PASSWORD": "restic-typed",
        "VPN_SERVICE_PROVIDER": "protonvpn", "OPENVPN_USER": "vpn-user", "OPENVPN_PASSWORD": "vpn-password", "SERVER_COUNTRIES": "Ireland",
        "JELLYFIN_ADMIN_PASSWORD": "jelly-typed", "DELUGE_WEB_PASSWORD": "deluge-typed", "PORTAINER_ADMIN_PASSWORD": "portainer-pass-long",
        "HOMEPAGE_PORT": "80",
    }


def test_a_new_installation_is_walked_through_every_question_then_saved_from_the_menu(menus, commands, tmp_path):
    folder = tmp_path / "backups"
    whiptail(commands, *guided_answers(folder), "0|Save")
    values = new()
    assert menus.fill(values, new=True)
    assert {name: values[name] for name in ("TZ", "CONFIG_LOCATION", "RESTIC_REPOSITORY", "OPENVPN_PASSWORD", "JELLYFIN_ADMIN_PASSWORD", "HEALTHCHECKS_PING_KEY", "HOMEPAGE_PORT")} == {
        "TZ": "Europe/London", "CONFIG_LOCATION": "local", "RESTIC_REPOSITORY": str(folder), "OPENVPN_PASSWORD": "vpn-password",
        "JELLYFIN_ADMIN_PASSWORD": "jelly-typed", "HEALTHCHECKS_PING_KEY": "", "HOMEPAGE_PORT": "80",
    }
    assert len(values["DELUGE_WEB_PASSWORD"]) == 32
    assert not any("B2 key ID" in screen for screen in whiptail_screens(commands))


def test_the_time_zone_is_picked_from_a_region_then_a_place_in_it(menus, commands, tmp_path):
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    menus.fill(new(), new=True)
    screens = whiptail_screens(commands)
    assert any("--menu Time zone" in screen and "Choose a region" in screen for screen in screens)
    assert any("--menu Choose a place in Europe" in screen and " London " in screen for screen in screens)


def test_utc_is_picked_without_a_place(menus, commands, tmp_path):
    whiptail(commands, "0|UTC", *guided_answers(tmp_path / "backups")[2:], "0|Save")
    values = new()
    menus.fill(values, new=True)
    assert values["TZ"] == "Etc/UTC"


def test_the_config_location_is_a_choice_that_says_it_can_be_switched_later(menus, commands, tmp_path):
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    menus.fill(new(), new=True)
    location = next(screen for screen in whiptail_screens(commands) if "--menu Where to keep this config" in screen)
    assert "You can switch at any time" in location
    assert "local Only on this machine github A private GitHub repo" in location


def test_back_returns_to_the_previous_question_that_applies(menus, commands, tmp_path):
    folder = tmp_path / "backups"
    whiptail(commands, "0|Europe", "0|London", "0|local", "0|admin", "1|", "0|admin2", *guided_answers(folder)[4:], "0|Save")
    values = new()
    assert menus.fill(values, new=True)
    assert values["JELLYFIN_ADMIN_USER"] == "admin2"


def test_back_on_the_first_question_asks_before_stopping_and_no_carries_on(menus, commands, tmp_path):
    whiptail(commands, "1|", "1|", *guided_answers(tmp_path / "backups"), "0|Save")
    assert menus.fill(new(), new=True)
    assert any("Stop configuring testinst? The answers so far are not saved." in screen for screen in whiptail_screens(commands))


def test_escape_asks_before_stopping_a_new_installation_and_yes_stops_it(menus, commands):
    whiptail(commands, "0|Europe", "0|London", "255|", "0|")
    with pytest.raises(menus.commands.Stop, match="stopped before testinst's settings were saved"):
        menus.fill(new(), new=True)


def test_discarding_from_a_new_installations_menu_asks_and_yes_stops_it(menus, commands, tmp_path):
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Discard", "0|")
    with pytest.raises(menus.commands.Stop, match="stopped before testinst's settings were saved"):
        menus.fill(new(), new=True)


def test_every_menu_says_which_keys_to_use(menus, commands, tmp_path):
    whiptail(commands, *guided_answers(tmp_path / "backups"), "0|Save")
    menus.fill(new(), new=True)
    shown_menus = [screen for screen in whiptail_screens(commands) if " --menu " in screen]
    assert shown_menus and all(KEYS_HINT in screen for screen in shown_menus)


def test_an_existing_installation_opens_on_the_menu_and_a_change_is_kept(menus, commands, tmp_path):
    values = existing(tmp_path / "backups")
    whiptail(commands, "0|VPN", "0|OPENVPN_PASSWORD", "0|new-vpn-password", "0|Back", "0|Save")
    assert menus.fill(values, new=False)
    assert values["OPENVPN_PASSWORD"] == "new-vpn-password"
    assert whiptail_screens(commands)[0].startswith("whiptail --title Configure testinst --cancel-button Discard --menu Choose a section to review or change.")


def test_the_menus_show_current_values_with_secrets_masked_and_back_and_discard_as_entries(menus, commands, tmp_path):
    whiptail(commands, "0|VPN", "0|Back", "0|Save")
    menus.fill(existing(tmp_path / "backups"), new=False)
    main_menu, vpn_menu = whiptail_screens(commands)[:2]
    assert "OpenVPN user: set, ends …ser" in vpn_menu and "OpenVPN password: set, ends …ord" in vpn_menu
    assert "vpn-user" not in vpn_menu and "vpn-password" not in vpn_menu
    assert "Back return to the sections" in vpn_menu
    assert "Discard leave without saving" in main_menu and "VPN all set" in main_menu
    assert "Healthchecks (optional) 3 not set" in main_menu


def test_discarding_an_existing_installations_changes_says_so(menus, commands, tmp_path):
    whiptail(commands, "0|VPN", "0|OPENVPN_USER", "0|someone-else", "0|Back", "0|Discard")
    assert not menus.fill(existing(tmp_path / "backups"), new=False)


def test_an_empty_password_box_keeps_the_current_secret(menus, commands, tmp_path):
    values = existing(tmp_path / "backups")
    whiptail(commands, "0|VPN", "0|OPENVPN_PASSWORD", "0|", "0|Back", "0|Save")
    menus.fill(values, new=False)
    assert values["OPENVPN_PASSWORD"] == "vpn-password"
    assert any("Leave empty to keep the current one." in screen for screen in whiptail_screens(commands))


def test_a_value_that_fails_its_check_is_explained_and_asked_again(menus, commands, tmp_path):
    answers = [answer if answer != "0|portainer-pass-long" else "0|short" for answer in guided_answers(tmp_path / "backups")]
    portainer = answers.index("0|short")
    whiptail(commands, *answers[: portainer + 1], "0|portainer-pass-long", *answers[portainer + 1:], "0|Save")
    values = new()
    menus.fill(values, new=True)
    assert any("--msgbox Portainer needs at least 12 characters." in screen for screen in whiptail_screens(commands))
    assert values["PORTAINER_ADMIN_PASSWORD"] == "portainer-pass-long"


def test_b2_details_are_asked_for_b2_and_rejected_ones_can_be_kept_anyway(menus, commands, tmp_path):
    commands.on(["restic"], done(returncode=12))
    b2 = ["0|Europe", "0|London", "0|local", "0|admin", "0|b2", "0|bucket", "0|restic", "0|0031keyid", "0|K005key", "0|restic-typed"]
    whiptail(commands, *b2, "0|", *guided_answers(tmp_path / "backups")[7:], "0|Save")
    values = new()
    assert menus.fill(values, new=True)
    assert any("does not open the backups already in b2:bucket:restic" in screen and "Keep it anyway?" in screen for screen in whiptail_screens(commands))
    assert (values["RESTIC_REPOSITORY"], values["B2_ACCOUNT_ID"]) == ("b2:bucket:restic", "0031keyid")


def test_rejected_b2_details_not_kept_return_to_the_first_backup_question(menus, commands, tmp_path):
    folder = tmp_path / "backups"
    commands.on(["restic"], done(returncode=12))
    b2 = ["0|Europe", "0|London", "0|local", "0|admin", "0|b2", "0|bucket", "0|restic", "0|0031keyid", "0|K005key", "0|restic-typed"]
    whiptail(commands, *b2, "1|", "0|local", f"0|{folder}", "0|restic-typed", *guided_answers(folder)[7:], "0|Save")
    values = new()
    assert menus.fill(values, new=True)
    assert (values["BACKUP_TYPE"], values["RESTIC_REPOSITORY"]) == ("local", str(folder))


def test_the_landing_page_has_its_own_section_in_the_menu(menus, commands, tmp_path):
    whiptail(commands, "0|Save")
    menus.fill(existing(tmp_path / "backups"), new=False)
    assert "Landing page all set" in whiptail_screens(commands)[0]


def test_a_message_is_shown_in_a_box(menus, commands):
    whiptail(commands)
    menus.message(new(), "Rotating Radarr's API key.", 10)
    assert whiptail_screens(commands) == ["whiptail --title Configure testinst --msgbox Rotating Radarr's API key. 10 76"]
