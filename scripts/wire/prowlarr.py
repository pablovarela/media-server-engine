import copy
import os

from wirelib import Api, WiringError, app_secrets, declared, fingerprint, remember, remembered, report, run

APP = "prowlarr"
PROWLARR_URL_SEEN_BY_APPS = "http://gluetun:9696"
UNREACHABLE = "Unable to connect to indexer"


def field(item, name):
    return next((f.get("value") for f in item["fields"] if f["name"] == name), None)


def set_field(item, name, value):
    for entry in item["fields"]:
        if entry["name"] == name:
            entry["value"] = value
            return
    item["fields"].append({"name": name, "value": value})


def by_name(items, name):
    return next((item for item in items if item["name"] == name), None)


class Prowlarr:
    def __init__(self, api, secrets):
        self.api = api
        self.secrets = secrets
        self.changed = False
        self.failures = []

    def change(self, description):
        report(APP, description)
        self.changed = True

    def schema(self, kind, key, value):
        found = next((s for s in self.api.get(f"/api/v1/{kind}/schema") if s.get(key) == value), None)
        if found is None:
            raise WiringError(f"no {kind} type {value}")
        return copy.deepcopy(found)

    def save(self, kind, item):
        try:
            if "id" in item:
                self.api.write("PUT", f"/api/v1/{kind}/{item['id']}?forceSave=true", item)
            else:
                self.api.write("POST", f"/api/v1/{kind}?forceSave=true", item)
            return True
        except WiringError as error:
            if kind == "indexer" and UNREACHABLE in str(error):
                report(APP, f"could not reach indexer {item['name']}; it is added at a later update once its site answers from this VPN location")
            else:
                self.failures.append(f"{item['name']}: {error}")
            return False

    def tag_id(self, label):
        tag = next((t for t in self.api.get("/api/v1/tag") if t["label"] == label), None)
        if tag is None:
            self.change(f"add tag {label}")
            tag = self.api.write("POST", "/api/v1/tag", {"label": label})
        return tag.get("id")

    def wire_proxies(self, proxies):
        tags = {}
        current = self.api.get("/api/v1/indexerproxy")
        for proxy in proxies:
            name = proxy["name"]
            tags[name] = self.tag_id(name.lower())
            item = by_name(current, name)
            if item is None:
                item = self.schema("indexerproxy", "implementation", proxy.get("type", name))
                item.update(name=name, tags=[tags[name]])
                set_field(item, "host", proxy["host"])
                self.change(f"add proxy {name}")
                self.save("indexerproxy", item)
                continue
            dirty = False
            if field(item, "host") != proxy["host"]:
                self.change(f"set proxy {name} host {field(item, 'host')} -> {proxy['host']}")
                set_field(item, "host", proxy["host"])
                dirty = True
            if tags[name] not in item["tags"]:
                self.change(f"tag proxy {name} {name.lower()}")
                item["tags"].append(tags[name])
                dirty = True
            if dirty:
                self.save("indexerproxy", item)
        return tags

    def wire_indexers(self, indexers, proxy_tags):
        current = self.api.get("/api/v1/indexer")
        profile_id = self.api.get("/api/v1/appprofile")[0]["id"]
        for indexer in indexers:
            name = indexer["name"]
            if indexer.get("proxy") and indexer["proxy"] not in proxy_tags:
                raise WiringError(f"indexer {name} uses proxy {indexer['proxy']}, which is not declared")
            wanted_tags = [proxy_tags[indexer["proxy"]]] if indexer.get("proxy") else []
            fields = indexer.get("fields") or {}
            item = by_name(current, name)
            if item is None:
                item = self.schema("indexer", "definitionName", indexer["definition"])
                item.update(name=name, enable=True, appProfileId=profile_id, tags=wanted_tags)
                if "priority" in indexer:
                    item["priority"] = indexer["priority"]
                for key, value in fields.items():
                    set_field(item, key, value)
                self.change(f"add indexer {name}")
                self.save("indexer", item)
                continue
            dirty = False
            if "priority" in indexer and item["priority"] != indexer["priority"]:
                self.change(f"set indexer {name} priority {item['priority']} -> {indexer['priority']}")
                item["priority"] = indexer["priority"]
                dirty = True
            for key, value in fields.items():
                if field(item, key) != value:
                    self.change(f"set indexer {name} {key} {field(item, key)} -> {value}")
                    set_field(item, key, value)
                    dirty = True
            for tag in wanted_tags:
                if tag not in item["tags"]:
                    self.change(f"tag indexer {name} for proxy {indexer['proxy']}")
                    item["tags"].append(tag)
                    dirty = True
            if dirty:
                self.save("indexer", item)

    def wire_applications(self, applications):
        current = self.api.get("/api/v1/applications")
        for application in applications:
            name = application["name"]
            key = self.secrets.get(application["api_key"])
            if not key:
                raise WiringError(f"{application['api_key']} is not in the app secrets")
            key_state = f"prowlarr-application-{name}.sha256"
            key_fingerprint = fingerprint(name, key)
            wanted = {"baseUrl": application["url"], "prowlarrUrl": PROWLARR_URL_SEEN_BY_APPS}
            if "sync_categories" in application:
                wanted["syncCategories"] = application["sync_categories"]
            item = by_name(current, name)
            if item is None:
                item = self.schema("applications", "implementation", application.get("type", name))
                item["name"] = name
                for key_name, value in wanted.items():
                    set_field(item, key_name, value)
                set_field(item, "apiKey", key)
                self.change(f"add application {name}")
                if self.save("applications", item):
                    remember(key_state, key_fingerprint)
                continue
            dirty = False
            for key_name, value in wanted.items():
                if field(item, key_name) != value:
                    self.change(f"set application {name} {key_name} {field(item, key_name)} -> {value}")
                    dirty = True
            if remembered(key_state) != key_fingerprint:
                self.change(f"set application {name} api key")
                dirty = True
            if dirty:
                for key_name, value in wanted.items():
                    set_field(item, key_name, value)
                set_field(item, "apiKey", key)
                if self.save("applications", item):
                    remember(key_state, key_fingerprint)

    def sync_indexers_to_applications(self):
        if self.changed:
            self.api.write("POST", "/api/v1/command", {"name": "ApplicationIndexerSync"})


def wire():
    config = declared("prowlarr.yml")
    secrets = app_secrets()
    api = Api(APP, os.environ.get("PROWLARR_URL", "http://localhost:9696"), {"X-Api-Key": secrets["PROWLARR_API_KEY"]})
    prowlarr = Prowlarr(api, secrets)
    proxy_tags = prowlarr.wire_proxies(config.get("indexer_proxies") or [])
    prowlarr.wire_indexers(config.get("indexers") or [], proxy_tags)
    prowlarr.wire_applications(config.get("applications") or [])
    prowlarr.sync_indexers_to_applications()
    if prowlarr.failures:
        raise WiringError("could not save " + "; ".join(prowlarr.failures))


if __name__ == "__main__":
    run(APP, wire)
