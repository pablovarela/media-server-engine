#!/usr/bin/env bash
set -uo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
cd "$MEDIA_SERVER_DIR" || exit 1

api_key() {
  sed -n 's:.*<ApiKey>\(.*\)</ApiKey>.*:\1:p' "$1"
}

QUEUE_PAGE_SIZE=${QUEUE_PAGE_SIZE:-200}

queue_total_and_flagged() {
  python3 -c '
import json, sys
queue = json.load(sys.stdin)
records = queue.get("records", [])
print(queue.get("totalRecords", len(records)))
for record in records:
    messages = [m for status in record.get("statusMessages", []) for m in status.get("messages", [])]
    if any("Found executable file" in m for m in messages):
        print(record["id"], record.get("downloadId") or record["id"], record.get("title", ""))
'
}

flagged_downloads() {
  local port=$1 key=$2 page=1 queue total
  while :; do
    queue=$(curl -fsS -m 30 -H "X-Api-Key: $key" "http://localhost:$port/api/v3/queue?page=$page&pageSize=$QUEUE_PAGE_SIZE") || return 1
    total=$(echo "$queue" | queue_total_and_flagged | head -1)
    echo "$queue" | queue_total_and_flagged | tail -n +2
    [ $((page * QUEUE_PAGE_SIZE)) -lt "$total" ] || return 0
    page=$((page + 1))
  done
}

remove_executable_downloads() {
  local app=$1 port=$2 key flagged id title
  key=$(api_key "$3")
  if ! flagged=$(flagged_downloads "$port" "$key"); then
    echo "$app: queue not reachable, skipped"
    return
  fi
  echo "$flagged" | awk 'NF && !seen[$2]++' | while read -r id _ title; do
    curl -fsS -m 30 -o /dev/null -X DELETE -H "X-Api-Key: $key" \
      "http://localhost:$port/api/v3/queue/$id?removeFromClient=true&blocklist=true&skipRedownload=false" \
      && echo "$app: removed and blocklisted $title"
  done
}

remove_executable_downloads sonarr 8989 volumes/sonarr/data/config.xml
remove_executable_downloads radarr 7878 volumes/radarr/config/config.xml
