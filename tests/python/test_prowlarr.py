import copy
import hashlib
import os
import stat

import pytest
import yaml

from conftest import REPO, http_error

PROWLARR_YML = """indexer_proxies:
- name: FlareSolverr
  host: http://localhost:8191/
indexers:
- name: The Pirate Bay
  definition: thepiratebay
  fields:
    apiurl: apibay.org
  proxy: FlareSolverr
  priority: 25
applications:
- name: Sonarr
  url: http://sonarr:8989
  api_key: SONARR_API_KEY
  sync_categories: [5000, 5010, 5020, 5030, 5040, 5045, 5050]
- name: Radarr
  url: http://radarr:7878
  api_key: RADARR_API_KEY
"""
SECRETS = "SONARR_API_KEY=sonarr-key\nRADARR_API_KEY=radarr-key\nPROWLARR_API_KEY=prowlarr-key\n"
RADARR_CATEGORIES = [2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080]


def fields(**values):
    return [{"name": name, "value": value} for name, value in values.items()]


PROXY_SCHEMA = {"implementation": "FlareSolverr", "name": "", "tags": [], "fields": fields(host="http://localhost:8191/", requestTimeout=60)}
INDEXER_SCHEMA = {"definitionName": "thepiratebay", "enable": True, "appProfileId": 0, "priority": 25, "tags": [], "fields": fields(definitionFile="thepiratebay", apiurl=None)}
APPLICATION_SCHEMAS = [
    {"implementation": name, "name": "", "fields": fields(prowlarrUrl="http://localhost:9696", baseUrl="http://localhost", apiKey=None, syncCategories=categories)}
    for name, categories in (("Sonarr", [5000, 5010, 5020, 5030, 5040, 5045, 5050]), ("Radarr", RADARR_CATEGORIES))
]
WIRED_PROXY = dict(copy.deepcopy(PROXY_SCHEMA), id=1, name="FlareSolverr", tags=[1])
WIRED_INDEXER = dict(copy.deepcopy(INDEXER_SCHEMA), id=1, name="The Pirate Bay", appProfileId=1, tags=[1], fields=fields(definitionFile="thepiratebay", apiurl="apibay.org"))
WIRED_APPLICATIONS = [
    {"id": 1, "name": "Sonarr", "implementation": "Sonarr", "fields": fields(prowlarrUrl="http://gluetun:9696", baseUrl="http://sonarr:8989", apiKey="********", syncCategories=[5000, 5010, 5020, 5030, 5040, 5045, 5050])},
    {"id": 2, "name": "Radarr", "implementation": "Radarr", "fields": fields(prowlarrUrl="http://gluetun:9696", baseUrl="http://radarr:7878", apiKey="********", syncCategories=RADARR_CATEGORIES)},
]


def value_of(item, name):
    return next(field["value"] for field in item["fields"] if field["name"] == name)


@pytest.fixture
def setup_prowlarr(wiring, dirs, monkeypatch):
    (dirs.config / "prowlarr.yml").write_text(PROWLARR_YML)
    monkeypatch.setenv("PROWLARR_URL", "http://prowlarr")

    def load(secrets=SECRETS, dry_run=False):
        return wiring("prowlarr", secrets, dry_run=dry_run)

    return load


@pytest.fixture
def prowlarr(setup_prowlarr):
    return setup_prowlarr()


def answering(http, proxies, indexers, applications, tags):
    http.on("GET", "/api/v1/indexerproxy", copy.deepcopy(proxies))
    http.on("GET", "/api/v1/indexerproxy/schema", [copy.deepcopy(PROXY_SCHEMA)])
    http.on("GET", "/api/v1/indexer", copy.deepcopy(indexers))
    http.on("GET", "/api/v1/indexer/schema", [copy.deepcopy(INDEXER_SCHEMA)])
    http.on("GET", "/api/v1/applications", copy.deepcopy(applications))
    http.on("GET", "/api/v1/applications/schema", copy.deepcopy(APPLICATION_SCHEMAS))
    http.on("GET", "/api/v1/appprofile", [{"id": 1, "name": "Standard"}])
    http.on("GET", "/api/v1/tag", tags)
    http.on("POST", "/api/v1/tag", {"id": 1, "label": "flaresolverr"})
    for kind in ("indexerproxy", "indexer", "applications"):
        http.on("POST", f"/api/v1/{kind}?forceSave=true", None)
    for path in ("indexerproxy/1", "indexer/1", "applications/1", "applications/2"):
        http.on("PUT", f"/api/v1/{path}?forceSave=true", None)
    http.on("POST", "/api/v1/command", None)


def fresh(http):
    answering(http, [], [], [], [])


def wired(http, indexer=None):
    answering(http, [WIRED_PROXY], [indexer or WIRED_INDEXER], WIRED_APPLICATIONS, [{"id": 1, "label": "flaresolverr"}])


def remember_applied_keys(dirs, sonarr="sonarr-key", radarr="radarr-key"):
    state = dirs.data / "volumes" / ".wiring"
    state.mkdir(parents=True, exist_ok=True)
    for name, key in (("Sonarr", sonarr), ("Radarr", radarr)):
        (state / f"prowlarr-application-{name}.sha256").write_text(hashlib.sha256(f"{name}\0{key}".encode()).hexdigest() + "\n")


def test_a_fresh_prowlarr_gets_the_proxy_indexers_and_applications_then_syncs(prowlarr, http, capsys):
    fresh(http)
    prowlarr.wire()
    assert http.writes() == [
        "POST /api/v1/tag",
        "POST /api/v1/indexerproxy?forceSave=true",
        "POST /api/v1/indexer?forceSave=true",
        "POST /api/v1/applications?forceSave=true",
        "POST /api/v1/applications?forceSave=true",
        "POST /api/v1/command",
    ]
    assert value_of(http.body("POST", "/api/v1/indexerproxy?forceSave=true"), "host") == "http://localhost:8191/"
    indexer = http.body("POST", "/api/v1/indexer?forceSave=true")
    assert (indexer["priority"], indexer["tags"], indexer["appProfileId"], value_of(indexer, "apiurl")) == (25, [1], 1, "apibay.org")
    sonarr, radarr = [request.body for request in http.requests if request.path == "/api/v1/applications?forceSave=true"]
    assert (value_of(sonarr, "apiKey"), value_of(sonarr, "prowlarrUrl"), value_of(sonarr, "syncCategories")) == ("sonarr-key", "http://gluetun:9696", [5000, 5010, 5020, 5030, 5040, 5045, 5050])
    assert (value_of(radarr, "baseUrl"), value_of(radarr, "syncCategories")) == ("http://radarr:7878", RADARR_CATEGORIES)
    assert http.body("POST", "/api/v1/command") == {"name": "ApplicationIndexerSync"}
    assert "prowlarr: add indexer The Pirate Bay" in capsys.readouterr().out


def test_the_wiring_signs_its_requests_with_prowlarrs_own_key(prowlarr, http, dirs):
    remember_applied_keys(dirs)
    wired(http)
    prowlarr.wire()
    assert {request.headers["X-api-key"] for request in http.requests} == {"prowlarr-key"}


def test_an_already_wired_prowlarr_is_left_untouched_including_indexers_it_was_not_told_about(prowlarr, http, dirs, capsys):
    remember_applied_keys(dirs)
    other = dict(copy.deepcopy(WIRED_INDEXER), id=9, name="Added by hand")
    answering(http, [WIRED_PROXY], [WIRED_INDEXER, other], WIRED_APPLICATIONS, [{"id": 1, "label": "flaresolverr"}])
    prowlarr.wire()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_a_drifted_priority_is_corrected_with_one_write_then_synced(prowlarr, http, dirs, capsys):
    remember_applied_keys(dirs)
    wired(http, indexer=dict(copy.deepcopy(WIRED_INDEXER), priority=50))
    prowlarr.wire()
    assert http.writes() == ["PUT /api/v1/indexer/1?forceSave=true", "POST /api/v1/command"]
    assert http.body("PUT", "/api/v1/indexer/1?forceSave=true")["priority"] == 25
    assert "prowlarr: set indexer The Pirate Bay priority 50 -> 25" in capsys.readouterr().out


def test_an_indexer_that_lost_its_proxy_tag_gets_it_back(prowlarr, http, dirs):
    remember_applied_keys(dirs)
    wired(http, indexer=dict(copy.deepcopy(WIRED_INDEXER), tags=[]))
    prowlarr.wire()
    assert http.body("PUT", "/api/v1/indexer/1?forceSave=true")["tags"] == [1]


def test_application_keys_are_applied_once_when_no_applied_key_is_remembered(setup_prowlarr, http, dirs):
    wired(http)
    setup_prowlarr().wire()
    assert [write for write in http.writes() if write.startswith("PUT /api/v1/applications/")] == ["PUT /api/v1/applications/1?forceSave=true", "PUT /api/v1/applications/2?forceSave=true"]
    assert value_of(http.body("PUT", "/api/v1/applications/1?forceSave=true"), "apiKey") == "sonarr-key"
    remembered = dirs.data / "volumes" / ".wiring" / "prowlarr-application-Sonarr.sha256"
    assert stat.S_IMODE(os.stat(remembered).st_mode) == 0o600
    http.requests.clear()
    setup_prowlarr().wire()
    assert http.writes() == []


def test_a_rotated_key_reaches_only_its_application(setup_prowlarr, http, dirs):
    remember_applied_keys(dirs)
    wired(http)
    setup_prowlarr(SECRETS.replace("SONARR_API_KEY=sonarr-key", "SONARR_API_KEY=rotated")).wire()
    assert [write for write in http.writes() if write.startswith("PUT")] == ["PUT /api/v1/applications/1?forceSave=true"]
    assert value_of(http.body("PUT", "/api/v1/applications/1?forceSave=true"), "apiKey") == "rotated"


def test_a_dry_run_reports_the_changes_writes_nothing_and_remembers_no_key(setup_prowlarr, http, dirs, capsys):
    fresh(http)
    setup_prowlarr(dry_run=True).wire()
    assert http.writes() == []
    assert "(dry run) prowlarr: add application Sonarr" in capsys.readouterr().out
    assert not (dirs.data / "volumes" / ".wiring").exists()


def test_an_item_prowlarr_rejects_fails_the_step_after_the_rest_is_wired(prowlarr, http):
    fresh(http)
    http.on("POST", "/api/v1/indexer?forceSave=true", http_error(400, '[{"errorMessage": "Invalid indexer settings"}]'))
    with pytest.raises(prowlarr.WiringError, match="could not save The Pirate Bay: .*Invalid indexer settings"):
        prowlarr.wire()
    assert http.writes().count("POST /api/v1/applications?forceSave=true") == 2


def test_an_indexer_prowlarr_cannot_reach_is_reported_and_added_at_a_later_update(prowlarr, http, capsys):
    fresh(http)
    http.on("POST", "/api/v1/indexer?forceSave=true", http_error(400, '[{"errorMessage": "Unable to connect to indexer. Unexpected response status UnavailableForLegalReasons"}]'))
    prowlarr.wire()
    assert "prowlarr: could not reach indexer The Pirate Bay; it is added at a later update" in capsys.readouterr().out
    assert http.writes().count("POST /api/v1/applications?forceSave=true") == 2


def test_an_indexer_naming_an_undeclared_proxy_fails_the_step_after_the_applications_are_wired(prowlarr, http, dirs):
    config = yaml.safe_load(PROWLARR_YML)
    config["indexers"][0]["proxy"] = "Flaresolver"
    (dirs.config / "prowlarr.yml").write_text(yaml.safe_dump(config))
    fresh(http)
    with pytest.raises(prowlarr.WiringError, match="indexer The Pirate Bay uses proxy Flaresolver, which is not declared"):
        prowlarr.wire()
    assert http.writes().count("POST /api/v1/applications?forceSave=true") == 2


def test_a_wrong_prowlarr_api_key_fails_with_prowlarrs_answer(prowlarr, http):
    http.on("GET", "/api/v1/indexerproxy", http_error(401, "Unauthorized"))
    with pytest.raises(prowlarr.WiringError, match="answered 401"):
        prowlarr.wire()


def test_the_template_declares_indexers_prowlarr_knows_behind_the_declared_proxy():
    template = yaml.safe_load((REPO / "config-template" / "prowlarr.yml").read_text())
    names = {indexer["name"] for indexer in template["indexers"]}
    assert {"1337x", "The Pirate Bay", "YTS", "LimeTorrents"} <= names
    proxies = {proxy["name"] for proxy in template["indexer_proxies"]}
    assert all(indexer.get("proxy") in {None} | proxies for indexer in template["indexers"])


TV_INDEXER = {"id": 1, "name": "TV and more", "enable": True, "capabilities": {"categories": [{"id": 3000, "subCategories": [{"id": 5040}]}]}}
MOVIES_INDEXER = {"id": 2, "name": "YTS", "enable": True, "capabilities": {"categories": [{"id": 2000, "subCategories": [{"id": 2040}]}]}}


@pytest.fixture
def sync(setup_prowlarr, dirs, monkeypatch):
    (dirs.config / "prowlarr.yml").write_text("applications:\n- name: Sonarr\n  url: http://sonarr:8989\n  api_key: SONARR_API_KEY\n")
    monkeypatch.setenv("SONARR_URL", "http://sonarr-here")
    monkeypatch.setenv("WIRE_SYNC_WAIT_SECONDS", "0")
    return setup_prowlarr


def answering_sync(http, indexers, in_sonarr):
    http.on("GET", "http://prowlarr/api/v1/indexer", indexers)
    http.on("GET", "http://prowlarr/api/v1/applications", [WIRED_APPLICATIONS[0]])
    http.on("POST", "http://prowlarr/api/v1/command", None)
    http.on("GET", "http://sonarr-here/api/v3/indexer", [{"id": number, "name": name} for number, name in enumerate(in_sonarr, start=1)])


def test_an_app_missing_indexers_prowlarr_would_send_it_is_synced_again(sync, http, capsys):
    prowlarr = sync()
    answering_sync(http, [TV_INDEXER], [])
    prowlarr.sync_again()
    assert http.writes("prowlarr") == ["POST /api/v1/command"]
    assert http.body("POST", "/api/v1/command", "prowlarr") == {"name": "ApplicationIndexerSync"}
    reported = capsys.readouterr().out
    assert "prowlarr: sync indexers again: Sonarr has 0 of 1" in reported
    assert "prowlarr: Sonarr still has 0 of prowlarr's 1 indexers; prowlarr will retry on its own schedule" in reported


def test_the_apps_indexers_are_counted_with_the_apps_own_key_at_its_address_on_this_machine(sync, http):
    prowlarr = sync()
    answering_sync(http, [TV_INDEXER], ["TV and more (Prowlarr)"])
    prowlarr.sync_again()
    [sonarr_request] = [request for request in http.requests if request.host == "sonarr-here"]
    assert sonarr_request.headers["X-api-key"] == "sonarr-key"


def test_apps_with_every_indexer_prowlarr_sends_them_are_left_alone(sync, http, capsys):
    prowlarr = sync()
    answering_sync(http, [TV_INDEXER, MOVIES_INDEXER], ["TV and more (Prowlarr)", "Added by hand"])
    prowlarr.sync_again()
    assert http.writes() == []
    assert capsys.readouterr().out == ""


def test_a_disabled_indexer_is_not_expected_in_any_app(sync, http):
    prowlarr = sync()
    answering_sync(http, [dict(TV_INDEXER, enable=False)], [])
    prowlarr.sync_again()
    assert http.writes() == []


def test_a_dry_run_reports_the_sync_and_asks_for_nothing(sync, http, capsys):
    prowlarr = sync(dry_run=True)
    answering_sync(http, [TV_INDEXER], [])
    prowlarr.sync_again()
    assert http.writes() == []
    assert "(dry run) prowlarr: sync indexers again: Sonarr has 0 of 1" in capsys.readouterr().out


def test_indexers_that_arrive_while_waiting_end_the_wait_without_a_note(sync, http, monkeypatch, capsys):
    prowlarr = sync()
    monkeypatch.setenv("WIRE_SYNC_WAIT_SECONDS", "60")
    monkeypatch.setattr(prowlarr.time, "sleep", lambda seconds: None)
    answering_sync(http, [TV_INDEXER], [])
    http.on("GET", "http://sonarr-here/api/v3/indexer", [], [{"id": 1, "name": "TV and more (Prowlarr)"}])
    prowlarr.sync_again()
    assert "still has" not in capsys.readouterr().out


@pytest.mark.parametrize(
    "declared, address",
    [("http://sonarr:8989", "http://localhost:8989"), ("http://sonarr", "http://localhost"), ("http://sonarr:8989/sonarr", "http://localhost:8989/sonarr")],
)
def test_an_apps_address_on_this_machine_keeps_the_declared_port_and_path(prowlarr, monkeypatch, declared, address):
    monkeypatch.delenv("SONARR_URL", raising=False)
    assert prowlarr.address_from_this_machine({"name": "Sonarr", "url": declared}) == address


@pytest.mark.parametrize(
    "typo, message, still_written",
    [
        (lambda config: config["indexers"][0].update(definition="thepiratebayy"), "indexer The Pirate Bay: no indexer type thepiratebayy", "POST /api/v1/applications?forceSave=true"),
        (lambda config: config["indexer_proxies"][0].update(type="Flaresolver"), "proxy FlareSolverr: no indexerproxy type Flaresolver", "POST /api/v1/applications?forceSave=true"),
        (lambda config: config["applications"][1].update(type="Radar"), "application Radarr: no applications type Radar", "POST /api/v1/indexer?forceSave=true"),
        (lambda config: config["applications"][0].update(api_key="SONAR_API_KEY"), "application Sonarr: SONAR_API_KEY is not in the app secrets", "POST /api/v1/applications?forceSave=true"),
    ],
    ids=["indexer definition", "proxy type", "application type", "application key name"],
)
def test_a_type_prowlarr_does_not_know_fails_the_step_after_the_rest_is_wired(prowlarr, http, dirs, typo, message, still_written):
    config = yaml.safe_load(PROWLARR_YML)
    typo(config)
    (dirs.config / "prowlarr.yml").write_text(yaml.safe_dump(config))
    fresh(http)
    with pytest.raises(prowlarr.WiringError, match=message):
        prowlarr.wire()
    assert still_written in http.writes()


def test_a_proxy_host_changed_in_the_config_is_updated(prowlarr, http, dirs, capsys):
    remember_applied_keys(dirs)
    (dirs.config / "prowlarr.yml").write_text(PROWLARR_YML.replace("http://localhost:8191/", "http://flaresolverr:8191/"))
    wired(http)
    prowlarr.wire()
    assert value_of(http.body("PUT", "/api/v1/indexerproxy/1?forceSave=true"), "host") == "http://flaresolverr:8191/"
    assert "prowlarr: set proxy FlareSolverr host http://localhost:8191/ -> http://flaresolverr:8191/" in capsys.readouterr().out


def test_an_indexer_field_changed_in_the_config_is_updated(prowlarr, http, dirs):
    remember_applied_keys(dirs)
    (dirs.config / "prowlarr.yml").write_text(PROWLARR_YML.replace("apiurl: apibay.org", "apiurl: apibay.example"))
    wired(http)
    prowlarr.wire()
    assert value_of(http.body("PUT", "/api/v1/indexer/1?forceSave=true"), "apiurl") == "apibay.example"


def test_an_application_address_changed_in_the_config_is_updated_with_its_real_key(prowlarr, http, dirs):
    remember_applied_keys(dirs)
    (dirs.config / "prowlarr.yml").write_text(PROWLARR_YML.replace("http://radarr:7878", "http://films:7878"))
    wired(http)
    prowlarr.wire()
    radarr = http.body("PUT", "/api/v1/applications/2?forceSave=true")
    assert (value_of(radarr, "baseUrl"), value_of(radarr, "apiKey")) == ("http://films:7878", "radarr-key")
