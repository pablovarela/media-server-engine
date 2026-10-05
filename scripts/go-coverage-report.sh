#!/usr/bin/env bash
set -euo pipefail

profile=${1:?usage: go-coverage-report.sh coverage.out}

awk -v module="github.com/pablovarela/media-server-engine/" '
  NR == 1 { next }
  {
    if (!($1 in statements)) statements[$1] = $2
    if ($3 > 0) hit[$1] = 1
  }
  END {
    for (block in statements) {
      file = block
      sub(/:.*/, "", file)
      if (index(file, module) == 1) file = substr(file, length(module) + 1)
      package = file
      sub(/\/[^\/]*$/, "", package)
      total[package] += statements[block]
      all += statements[block]
      if (block in hit) { covered[package] += statements[block]; done += statements[block] }
    }
    printf "total\t%.1f\n", all ? 100 * done / all : 100
    for (package in total) {
      if (covered[package] < total[package]) {
        printf "row\t%.1f\t%s\t%d\t%d\n", 100 * covered[package] / total[package], package, total[package] - covered[package], total[package]
      }
    }
  }' "$profile" | sort -t$'\t' -k1,1r -k2,2n -k3,3 | awk -F'\t' '
  $1 == "total" { print "### Go test coverage"; print ""; printf "Total: %s%% of statements.\n", $2; next }
  !header { print ""; print "Packages not fully covered:"; print ""; print "| Package | Coverage | Not covered |"; print "|---|---|---|"; header = 1 }
  { printf "| %s | %s%% | %s of %s |\n", $3, $2, $4, $5 }
  END { if (!header) { print ""; print "Every package is fully covered." } }'
