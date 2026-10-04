#!/usr/bin/env bash
set -euo pipefail

tag=$1
config=$2

if [[ ! $tag =~ ^v([0-9]+)\.[0-9]+\.[0-9]+$ ]]; then
  echo "$tag is not a vMAJOR.MINOR.PATCH tag"
  exit 1
fi
major=${BASH_REMATCH[1]}
schema=$(sed -n 's/^config: *\([0-9][0-9]*\) *$/\1/p' "$config")

if [ "$schema" != "$major" ]; then
  echo "$tag is major $major but $config declares config: ${schema:-nothing}; a new major bumps both"
  exit 1
fi
