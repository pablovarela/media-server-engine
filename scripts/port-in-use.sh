#!/usr/bin/env bash
set -uo pipefail
port=$1

something_listens() {
  python3 -c '
import socket, sys
with socket.socket() as probe:
    probe.settimeout(1)
    sys.exit(0 if probe.connect_ex(("127.0.0.1", int(sys.argv[1]))) == 0 else 1)' "$port"
}

this_landing_page_listens() {
  docker port homepage 3000 2>/dev/null | grep -q ":$port$"
}

something_listens && ! this_landing_page_listens
