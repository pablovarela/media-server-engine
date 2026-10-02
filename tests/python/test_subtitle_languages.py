import pytest
import yaml

import subtitle_languages


@pytest.fixture
def apps(tmp_path):
    return tmp_path / "apps.yml"


def set_languages(apps, wanted):
    subtitle_languages.main(str(apps), wanted)


def languages_read_back(apps):
    return yaml.safe_load(apps.read_text())["bazarr"]["languages"]


def test_changing_the_languages_changes_only_that_line_keeping_its_comment_and_the_rest(apps):
    before = "# deluge stays seeding for two days\ndeluge:\n  seed: 48\nbazarr:\n  languages: [en]  # what bazarr fetches\n"
    apps.write_text(before)
    set_languages(apps, "en, es")
    assert apps.read_text() == before.replace("languages: [en]  #", "languages: [en, es]  #")


def test_languages_written_as_a_block_list_are_replaced_in_place(apps):
    apps.write_text("bazarr:\n  languages:\n    - en\n    - fr\n  providers: [x]\nmaintainerr: {}\n")
    set_languages(apps, "es")
    assert apps.read_text() == "bazarr:\n  languages: [es]\n  providers: [x]\nmaintainerr: {}\n"


def test_a_bazarr_section_without_languages_gets_the_line_at_its_indentation(apps):
    apps.write_text("bazarr:\n    providers: [x]\n")
    set_languages(apps, "en")
    assert apps.read_text() == "bazarr:\n    languages: [en]\n    providers: [x]\n"


def test_a_file_without_a_bazarr_section_gets_one(apps):
    apps.write_text("seerr:\n  libraries: [Shows]\n")
    set_languages(apps, "en, es")
    assert apps.read_text() == "seerr:\n  libraries: [Shows]\nbazarr:\n  languages: [en, es]\n"


def test_languages_already_as_wanted_leave_the_file_untouched(apps):
    before = "bazarr:\n  languages:   [en]   # spaced by hand\n"
    apps.write_text(before)
    set_languages(apps, "en")
    assert apps.read_text() == before


def test_a_code_yaml_would_read_as_something_other_than_text_is_quoted_so_it_stays_a_language(apps):
    apps.write_text("bazarr:\n  languages: [en]\n")
    set_languages(apps, "en, no")
    assert languages_read_back(apps) == ["en", "no"]
    assert "  languages: [en, 'no']\n" in apps.read_text()


@pytest.mark.parametrize(
    "layout",
    [
        pytest.param("bazarr: {languages: [en], providers: [x]}\n", id="bazarr written on one line"),
        pytest.param("bazarr:\n  languages: [en,\n    fr]\n", id="languages spread over several lines"),
    ],
)
def test_a_layout_it_cannot_edit_line_by_line_is_left_alone_with_a_hint(apps, layout):
    apps.write_text(layout)
    with pytest.raises(SystemExit) as stopped:
        set_languages(apps, "en, es")
    assert "set bazarr.languages to [en, es] by hand" in str(stopped.value)
    assert apps.read_text() == layout


def test_a_code_yaml_cannot_read_at_all_is_quoted_too(apps):
    apps.write_text("bazarr:\n  languages: [en]\n")
    set_languages(apps, "en, [")
    assert languages_read_back(apps) == ["en", "["]
