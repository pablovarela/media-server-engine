import json
import os
import re
import shutil

import pytest
import yaml

from conftest import REPO, answer, fresh_import

ENGINE_PAGE = REPO / "homepage"


class Page:
    def __init__(self, dirs, homepage):
        self.dirs = dirs
        self.homepage = homepage
        self.out = dirs.engine / ".homepage"
        self.images = dirs.engine / ".homepage-images"

    def render(self):
        self.homepage.render(str(self.out))

    def file(self, name):
        return (self.out / name).read_text()

    def yaml(self, name):
        return yaml.safe_load(self.file(name))

    def groups(self):
        return {next(iter(group)): next(iter(group.values())) for group in self.yaml("services.yaml")}

    def group_names(self):
        return [next(iter(group)) for group in self.yaml("services.yaml")]

    def tiles(self, group):
        return [next(iter(tile)) for tile in self.groups().get(group, [])]

    def health_tiles(self):
        return [
            (name, options["widget"]["url"].split("slug=")[1], options["widget"]["mappings"][0]["field"])
            for tile in self.groups()["Healthchecks"]
            for name, options in tile.items()
        ]

    def config_file(self, name, text):
        (self.dirs.config / "homepage").mkdir(exist_ok=True)
        (self.dirs.config / "homepage" / name).write_text(text)


@pytest.fixture
def page(dirs, monkeypatch):
    shutil.copytree(ENGINE_PAGE, dirs.engine / "homepage")
    secrets = dirs.engine / ".secrets"
    secrets.mkdir()
    (secrets / "apps.env").write_text(
        "SONARR_API_KEY=s1\nRADARR_API_KEY=r1\nPROWLARR_API_KEY=p1\nDELUGE_WEB_PASSWORD=d pass\nPORTAINER_ADMIN_PASSWORD=never\n"
    )
    wiring = dirs.data / "volumes" / ".wiring"
    wiring.mkdir(parents=True)
    (wiring / "jellyfin.key").write_text("jellyfin-key\n")
    (wiring / "gluetun-control.key").write_text("gluetun-key\n")
    seerr = dirs.data / "volumes" / "seerr" / "config"
    seerr.mkdir(parents=True)
    (seerr / "settings.json").write_text('{"main": {"apiKey": "seerr-key"}}')
    bazarr = dirs.data / "volumes" / "bazarr" / "config" / "config"
    bazarr.mkdir(parents=True)
    (bazarr / "config.yaml").write_text("auth:\n  apikey: bazarr-key\n")
    monkeypatch.setenv("INSTALLATION_NAME", "testinst")
    monkeypatch.setenv("HOMEPAGE_HOST", "media.local")
    monkeypatch.setenv("HOMEPAGE_ENGINE_VERSION", "v9.9.9")
    return Page(dirs, fresh_import("homepage"))


@pytest.fixture
def checks(page, urlopen):
    def answer_with(*slugs):
        urlopen.side_effect = None
        urlopen.return_value = answer({"checks": [{"slug": slug, "status": "up"} for slug in slugs]})
        with_read_only_key(page)

    return answer_with


def with_read_only_key(page):
    (page.dirs.engine / ".secrets" / "healthchecks.env").write_text("HEALTHCHECKS_API_KEY=hc-read\n")


def test_the_default_page_names_the_installation_and_the_engine_version_and_links_to_this_machine(page):
    page.render()
    assert page.yaml("settings.yaml")["title"] == "testinst"
    greetings = [options["text"] for widget in page.yaml("widgets.yaml") for options in widget.values() if isinstance(options, dict) and "text" in options]
    assert "testinst" in greetings
    assert "engine v9.9.9" in greetings
    assert "href: http://media.local:8989" in page.file("services.yaml")
    for name in ("settings.yaml", "services.yaml", "widgets.yaml", "bookmarks.yaml"):
        assert not re.search(r"@[A-Z_]+@", page.file(name)), name


def test_backup_status_is_shown_only_with_a_read_only_healthchecks_api_key(page, checks, urlopen):
    page.render()
    assert "Healthchecks" not in page.file("services.yaml")
    urlopen.assert_not_called()
    checks("testinst-backup", "testinst-update", "testinst-verify")
    page.render()
    assert page.group_names()[0] == "Healthchecks"


def test_each_health_check_has_its_own_tile_which_asks_healthchecks_for_that_check_by_name(page, checks):
    checks("testinst-update", "other-backup", "testinst-verify", "testinst-backup")
    page.render()
    assert page.health_tiles() == [
        ("Backup", "testinst-backup", "checks.0.status"),
        ("Update", "testinst-update", "checks.0.status"),
        ("Verify", "testinst-verify", "checks.0.status"),
    ]


def test_the_checks_are_read_with_the_read_only_key(page, checks, urlopen):
    checks("testinst-backup")
    page.render()
    request = urlopen.call_args.args[0]
    assert request.full_url == "https://healthchecks.io/api/v3/checks/"
    assert request.get_header("X-api-key") == "hc-read"


def test_a_check_healthchecks_does_not_have_yet_gets_no_tile(page, checks):
    checks("testinst-update")
    page.render()
    assert page.health_tiles() == [("Update", "testinst-update", "checks.0.status")]


def test_a_secondary_machines_page_shows_its_own_update_check(page, checks, monkeypatch):
    checks("testinst-backup", "testinst-update", "testinst-update-pi2")
    monkeypatch.setenv("HOMEPAGE_HEALTHCHECK_UPDATE", "testinst-update-pi2")
    page.render()
    assert page.health_tiles() == [
        ("Backup", "testinst-backup", "checks.0.status"),
        ("Update", "testinst-update-pi2", "checks.0.status"),
    ]


def test_the_health_check_tiles_show_only_their_status_with_the_tile_titles_hidden(page):
    page.render()
    assert 'li[id^="healthchecks-"] .service-title' in page.file("custom.css")
    engine_groups = yaml.safe_load((ENGINE_PAGE / "services.yaml").read_text())
    health = next(group["Healthchecks"] for group in engine_groups if "Healthchecks" in group)
    assert [options["id"] for tile in health for options in tile.values()] == ["healthchecks-backup", "healthchecks-update", "healthchecks-verify"]


def test_without_any_of_the_installations_checks_the_health_checks_are_not_on_the_page(page, checks):
    checks("other-backup")
    page.render()
    assert "Healthchecks" not in page.file("services.yaml")
    assert page.group_names()[0] == "Coming up"


def test_when_healthchecks_cannot_be_reached_the_page_is_drawn_without_the_health_checks(page, urlopen, capsys):
    urlopen.side_effect = OSError("connection refused")
    with_read_only_key(page)
    page.render()
    assert "could not read the checks from healthchecks" in capsys.readouterr().err
    assert "Healthchecks" not in page.file("services.yaml")
    assert "Sonarr" in page.file("services.yaml")


def test_the_health_checks_lead_the_page_without_a_heading_under_a_boxed_header(page, checks):
    checks("testinst-backup", "testinst-update", "testinst-verify")
    page.render()
    settings = page.yaml("settings.yaml")
    assert settings["headerStyle"] == "boxed"
    layout = settings["layout"]
    assert layout[0] == {"Healthchecks": {"style": "row", "columns": 3, "header": False}}
    assert [next(iter(group)) for group in layout] == ["Healthchecks", "Coming up", "Watch", "Downloads", "Library", "Maintenance"]
    assert all(next(iter(group.values())).get("style") == "columns" for group in layout[3:])


def test_the_pages_environment_holds_the_keys_its_widgets_use_and_nothing_else(page, capsys):
    with_read_only_key(page)
    page.homepage.env()
    assert capsys.readouterr().out.splitlines() == [
        "HOMEPAGE_VAR_SONARR_KEY=s1",
        "HOMEPAGE_VAR_RADARR_KEY=r1",
        "HOMEPAGE_VAR_PROWLARR_KEY=p1",
        "HOMEPAGE_VAR_DELUGE_PASSWORD=d pass",
        "HOMEPAGE_VAR_JELLYFIN_KEY=jellyfin-key",
        "HOMEPAGE_VAR_SEERR_KEY=seerr-key",
        "HOMEPAGE_VAR_BAZARR_KEY=bazarr-key",
        "HOMEPAGE_VAR_GLUETUN_KEY=gluetun-key",
        "HOMEPAGE_VAR_HEALTHCHECKS_KEY=hc-read",
    ]


def test_keys_that_do_not_exist_yet_before_the_first_wiring_are_left_empty(page, capsys):
    os.remove(page.dirs.data / "volumes" / ".wiring" / "jellyfin.key")
    os.remove(page.dirs.data / "volumes" / "seerr" / "config" / "settings.json")
    page.homepage.env()
    lines = capsys.readouterr().out.splitlines()
    assert "HOMEPAGE_VAR_JELLYFIN_KEY=" in lines
    assert "HOMEPAGE_VAR_SEERR_KEY=" in lines


def test_the_default_page_itself_is_reached_at_the_landing_pages_port_the_apps_at_theirs(page, monkeypatch):
    monkeypatch.setenv("HOMEPAGE_PORT", "8080")
    page.render()
    assert "href: http://media.local:8989" in page.file("services.yaml")


def test_the_default_page_has_no_bookmarks_so_homepages_sample_links_are_not_added(page):
    page.render()
    assert page.yaml("bookmarks.yaml") == []


def test_recently_added_authenticates_the_way_current_jellyfin_accepts(page):
    page.render()
    watch = page.groups()["Watch"]
    widget = next(tile["Recently added"]["widget"] for tile in watch if "Recently added" in tile)
    assert widget["headers"] == {"Authorization": 'MediaBrowser Token="{{HOMEPAGE_VAR_JELLYFIN_KEY}}"'}


def test_what_is_coming_up_leads_the_page_an_agenda_next_to_a_months_calendar(page):
    page.render()
    assert page.group_names()[:2] == ["Coming up", "Watch"]

    def calendar_views(group):
        return [
            (name, options["widget"]["view"])
            for tile in page.groups().get(group, [])
            for name, options in tile.items()
            if (options.get("widget") or {}).get("type") == "calendar"
        ]

    assert calendar_views("Coming up") == [("Next up", "agenda"), ("Calendar", "monthly")]
    assert calendar_views("Library") == []


def test_the_engine_version_links_to_its_release(page, monkeypatch):
    monkeypatch.setenv("HOMEPAGE_ENGINE_URL", "https://github.com/someone/media-server-engine/releases/tag/v9.9.9")
    page.render()
    assert '"href": "https://github.com/someone/media-server-engine/releases/tag/v9.9.9"' in json.dumps(page.yaml("widgets.yaml"))


def test_without_a_known_engine_address_the_version_is_plain_text(page):
    page.render()
    assert "href" not in page.file("widgets.yaml")


def test_a_config_holding_a_copy_of_the_engines_page_renders_the_same_page(page):
    page.render()
    names = ("settings.yaml", "services.yaml", "widgets.yaml", "bookmarks.yaml")
    engine_rendering = {name: page.file(name) for name in names}
    for name in names:
        page.config_file(name, (ENGINE_PAGE / name).read_text())
    page.render()
    assert {name: page.file(name) for name in names} == engine_rendering


def test_the_configs_files_can_use_the_same_placeholders_as_the_engines(page):
    page.config_file("services.yaml", "- Home:\n    - Router:\n        href: http://@HOST@:8443\n")
    page.config_file("widgets.yaml", '- greeting:\n    text: "@INSTALLATION_NAME@ on @HOST@"\n')
    page.render()
    assert "href: http://media.local:8443" in page.file("services.yaml")
    assert "testinst on media.local" in page.file("widgets.yaml")


def test_backup_status_stays_off_the_page_without_an_api_key_even_when_the_config_declares_it(page):
    page.config_file("services.yaml", (ENGINE_PAGE / "services.yaml").read_text())
    page.render()
    assert "Healthchecks" not in page.file("services.yaml")


def test_a_file_in_the_config_is_the_pages_file_exactly_as_written(page):
    page.config_file("settings.yaml", "title: mine\ntheme: light\n")
    page.config_file("services.yaml", "- Home:\n    - Router:\n        href: http://192.168.1.1\n")
    page.config_file("widgets.yaml", "- datetime:\n    text_size: xl\n")
    page.config_file("bookmarks.yaml", "- Links:\n    - Docs:\n        - href: https://gethomepage.dev\n")
    page.config_file("custom.css", "html { font-size: 20px; }\n")
    page.render()
    settings = page.yaml("settings.yaml")
    assert (settings["title"], settings["theme"]) == ("mine", "light")
    assert page.group_names() == ["Home"]
    assert [next(iter(widget)) for widget in page.yaml("widgets.yaml")] == ["datetime"]
    assert "gethomepage.dev" in page.file("bookmarks.yaml")
    assert page.file("custom.css") == "html { font-size: 20px; }\n"


def test_a_tile_commented_out_in_the_config_is_not_on_the_page(page):
    page.config_file(
        "services.yaml",
        "- Coming up:\n    - Next up:\n        icon: mdi-calendar-clock\n#    - Calendar:\n#        icon: mdi-calendar-month\n",
    )
    page.render()
    assert page.tiles("Coming up") == ["Next up"]


def test_a_file_the_config_does_not_have_comes_from_the_engine(page):
    page.config_file("settings.yaml", "title: mine\n")
    page.render()
    assert page.group_names()[0] == "Coming up"
    assert "html { font-size: 18px; }" in page.file("custom.css")


def test_backup_status_stays_off_the_page_without_an_api_key_also_inside_a_nested_group(page):
    page.config_file(
        "services.yaml",
        "- Manage:\n    - Maintenance:\n        - Portainer:\n            href: http://@HOST@:9000\n"
        "        - Healthchecks:\n            widget:\n              type: healthchecks\n",
    )
    page.render()
    assert "Healthchecks" not in page.file("services.yaml")
    assert "Portainer" in page.file("services.yaml")


def test_images_in_the_configs_homepage_folder_are_served_by_the_page(page):
    images = page.dirs.config / "homepage" / "images"
    images.mkdir(parents=True)
    (images / "background.svg").write_text("<svg/>\n")
    page.images.mkdir()
    (page.images / "gone.png").write_text("old\n")
    page.render()
    assert (page.images / "background.svg").read_text() == "<svg/>\n"
    assert not (page.images / "gone.png").exists()


def test_a_redraw_keeps_the_images_folder_itself_which_homepage_has_mounted(page):
    page.images.mkdir()
    before = page.images.stat().st_ino
    images = page.dirs.config / "homepage" / "images"
    images.mkdir(parents=True)
    (images / "a.svg").write_text("<svg/>\n")
    page.render()
    assert page.images.stat().st_ino == before


def test_blank_lines_in_a_secrets_file_are_skipped(page, capsys):
    (page.dirs.engine / ".secrets" / "apps.env").write_text("\nSONARR_API_KEY=s1\n\nRADARR_API_KEY=r1\n")
    page.homepage.env()
    lines = capsys.readouterr().out.splitlines()
    assert "HOMEPAGE_VAR_SONARR_KEY=s1" in lines
    assert "HOMEPAGE_VAR_RADARR_KEY=r1" in lines


def test_a_page_without_health_check_markers_is_drawn_without_asking_healthchecks(page, urlopen):
    with_read_only_key(page)
    page.config_file("services.yaml", "- Home:\n    - Router:\n        href: http://@HOST@:8443\n")
    page.render()
    urlopen.assert_not_called()
    assert page.tiles("Home") == ["Router"]



def test_folders_in_the_configs_images_are_served_and_old_ones_removed(page):
    icons = page.dirs.config / "homepage" / "images" / "icons"
    icons.mkdir(parents=True)
    (icons / "router.svg").write_text("<svg/>\n")
    (page.images / "old-folder").mkdir(parents=True)
    (page.images / "old-folder" / "x.png").write_text("old\n")
    page.render()
    assert (page.images / "icons" / "router.svg").read_text() == "<svg/>\n"
    assert not (page.images / "old-folder").exists()
