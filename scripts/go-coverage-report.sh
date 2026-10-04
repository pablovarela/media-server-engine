#!/usr/bin/env bash
set -euo pipefail

awk -F'\t+' -v module="github.com/pablovarela/media-server-engine/" '
  index($1, module) == 1 { $1 = substr($1, length(module) + 1) }
  $1 == "total:" { total = $NF; next }
  $NF != "100.0%" { sub(/:$/, "", $1); rows = rows sprintf("| `%s` | %s | %s |\n", $2, $1, $NF) }
  END {
    print "### Go test coverage"
    print ""
    printf "Total: %s of statements.\n", total
    print ""
    if (rows == "") { print "Every function is fully covered."; exit }
    print "Functions not fully covered:"
    print ""
    print "| Function | Where | Coverage |"
    print "|---|---|---|"
    printf "%s", rows
  }'
