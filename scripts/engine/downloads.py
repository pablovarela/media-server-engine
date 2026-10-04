import http.client
import json
import os
import re
import sys
import urllib.request

from engine import installation, program

APPS = [("sonarr", 8989, "volumes/sonarr/data/config.xml"), ("radarr", 7878, "volumes/radarr/config/config.xml")]
FLAG = "Found executable file"


def api_key(path):
    try:
        with open(path) as config:
            found = re.search(r"<ApiKey>(.*)</ApiKey>", config.read())
    except OSError:
        return ""
    return found.group(1) if found else ""


def call(method, port, key, path):
    request = urllib.request.Request(f"http://localhost:{port}{path}", method=method)
    request.add_header("X-Api-Key", key)
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.read()


def is_flagged(record):
    return any(FLAG in message for status in record.get("statusMessages", []) for message in status.get("messages", []))


def flagged_downloads(port, key):
    size = int(os.environ.get("QUEUE_PAGE_SIZE") or 200)
    page, found = 1, []
    while True:
        queue = json.loads(call("GET", port, key, f"/api/v3/queue?page={page}&pageSize={size}"))
        records = queue.get("records", [])
        found += [(record["id"], record.get("downloadId") or record["id"], record.get("title", "")) for record in records if is_flagged(record)]
        if page * size >= queue.get("totalRecords", len(records)):
            return found
        page += 1


def remove_executable_downloads(app, port, config):
    key = api_key(os.path.join(installation.data_dir(), config))
    if not key:
        print(f"{app}: queue not reachable, skipped")
        return
    try:
        found = flagged_downloads(port, key)
    except (OSError, http.client.HTTPException, ValueError, KeyError, TypeError, AttributeError):
        print(f"{app}: queue not reachable, skipped")
        return
    removed = set()
    for record_id, download_id, title in found:
        if download_id in removed:
            continue
        removed.add(download_id)
        try:
            call("DELETE", port, key, f"/api/v3/queue/{record_id}?removeFromClient=true&blocklist=true&skipRedownload=false")
        except (OSError, http.client.HTTPException) as error:
            print(f"{app}: could not remove {title}: {getattr(error, 'reason', error)}", file=sys.stderr)
            continue
        print(f"{app}: removed and blocklisted {title}")


def clean(argv):
    for app in APPS:
        remove_executable_downloads(*app)


def main(argv):
    return program.run("remove-executable-downloads", clean, argv)
