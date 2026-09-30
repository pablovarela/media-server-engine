#!/usr/bin/env bash
set -uo pipefail
# shellcheck source=scripts/lib.sh
source "$(dirname "$0")/lib.sh"
# shellcheck source=scripts/tool-versions.env
source "$(dirname "$0")/tool-versions.env"
cd "$CONFIG_DIR" || exit 1

if [ "$(uname -s)" = Linux ]; then
  CHECK_PINNED_VERSIONS=${CHECK_PINNED_VERSIONS:-1}
else
  CHECK_PINNED_VERSIONS=${CHECK_PINNED_VERSIONS:-0}
fi

problems=0

report() {
  printf '%-8s %s\n' "$1" "$2"
}

problem() {
  report "$1" "$2"
  problems=$((problems + 1))
}

installed() {
  command -v "$1" >/dev/null
}

check_command() {
  if installed "$1"; then report OK "$1"; else problem MISSING "$1"; fi
}

check_pinned_version() {
  local name=$1 pinned=$2 version_flag=$3 installed_version
  installed "$name" || return 0
  [ "$CHECK_PINNED_VERSIONS" = 1 ] || return 0
  installed_version=$("$name" "$version_flag" 2>&1 | head -1)
  if reports_version "$installed_version" "$pinned"; then
    report OK "$name $pinned"
  else
    problem WRONG "$name: pinned $pinned, installed ${installed_version:-unknown}"
  fi
}

reports_version() {
  local word
  for word in $1; do
    [ "${word#v}" = "${2#v}" ] && return 0
  done
  return 1
}

check_compose_plugin() {
  if docker compose version >/dev/null 2>&1; then report OK "docker compose"; else problem MISSING "docker compose plugin"; fi
}

check_python_yaml() {
  if python3 -c 'import yaml' 2>/dev/null; then report OK "python3 yaml module"; else problem MISSING "python3 yaml module"; fi
}

check_optional_menus() {
  if installed "${WHIPTAIL_COMMAND:-whiptail}"; then
    report OK whiptail
  else
    report OPTIONAL "whiptail (not installed: make configure asks plain questions instead of menus)"
  fi
}

check_secrets_key() {
  if sops decrypt secrets/vpn.sops.env >/dev/null 2>&1; then
    report OK "age key decrypts secrets"
  else
    problem MISSING "age key that decrypts secrets (${SOPS_AGE_KEY_FILE:-default sops key location})"
  fi
}

for tool in docker git make curl sqlite3 python3 sops age restic; do
  check_command "$tool"
done
installed docker && check_compose_plugin
installed python3 && check_python_yaml
check_optional_menus
check_pinned_version sops "$SOPS_VERSION" --version
check_pinned_version age "$AGE_VERSION" --version
check_pinned_version restic "$RESTIC_VERSION" version
installed sops && check_secrets_key

if [ "$problems" -gt 0 ]; then
  echo "$problems problem(s). make bootstrap installs the tools; the age key comes from the password manager." >&2
  exit 1
fi
