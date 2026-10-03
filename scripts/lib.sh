# shellcheck shell=bash
ENGINE_DIR=${ENGINE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}
CONFIG_DIR=${CONFIG_DIR:-$(dirname "$ENGINE_DIR")/config}
DATA_DIR=${DATA_DIR:-$(dirname "$ENGINE_DIR")/data}
export ENGINE_DIR CONFIG_DIR DATA_DIR

# The healthchecks.io API keys are read from the decrypted secrets file; only the ping key belongs in the environment.
unset HEALTHCHECKS_API_KEY HEALTHCHECKS_MANAGE_KEY

export SNAPSHOT_VOLUMES_PATH=/volumes

die() {
  echo "$(basename "$0"): $*" >&2
  exit 1
}

installations_on_this_machine() {
  local config
  for config in "$HOME"/*/config/installation.env; do
    if [ -f "$config" ] && [ -d "$(dirname "$(dirname "$config")")/engine" ]; then
      dirname "$(dirname "$config")"
    fi
  done
}

die_not_an_installation() {
  local found
  found=$(installations_on_this_machine)
  {
    echo "$ENGINE_DIR is not an installation: there is no config next to it."
    if [ -n "$found" ]; then
      echo "Installations on this machine; run make from the one you mean:"
      echo "$found" | while IFS= read -r installation; do echo "  cd $installation"; done
    fi
    echo "To create one: make create-installation NAME=<name>"
  } >&2
  exit 1
}

installation_root() {
  local root
  root=$(cd "$ENGINE_DIR/.." && pwd -P)
  [ "$(basename "$ENGINE_DIR")" = engine ] && [ "$(cd "$CONFIG_DIR" 2>/dev/null && pwd -P)" = "$root/config" ] && echo "$root"
}

write_installation_makefile() {
  local root
  root=$(installation_root) || return 0
  cmp -s "$ENGINE_DIR/installation/Makefile" "$root/Makefile" || cp "$ENGINE_DIR/installation/Makefile" "$root/Makefile"
}

require_installation() {
  [ -f "$CONFIG_DIR/installation.env" ] || die_not_an_installation
}

load_installation() {
  [ -f "$CONFIG_DIR/installation.env" ] || die_not_an_installation
  set -a
  # shellcheck source=/dev/null
  source "$CONFIG_DIR/installation.env"
  set +a
  [ -n "${INSTALLATION_NAME:-}" ] || die "INSTALLATION_NAME is not set in $CONFIG_DIR/installation.env"
  export INSTALLATION_NAME
}

compose_files() {
  local base=$1 images=$2
  printf -- '-f %s -f %s ' "$ENGINE_DIR/$base" "$CONFIG_DIR/$images"
  if [ "$base" = docker-compose.yml ] && [ -f "$CONFIG_DIR/compose.override.yml" ]; then
    printf -- '-f %s ' "$CONFIG_DIR/compose.override.yml"
  fi
}

optional_services_pinned() {
  python3 -c '
import sys, yaml
services = (yaml.safe_load(open(sys.argv[1])) or {}).get("services") or {}
print(",".join(name for name in ("homepage",) if (services.get(name) or {}).get("image")))
' "$CONFIG_DIR/images.yml" 2>/dev/null || true
}

homepage_port() {
  echo "${HOMEPAGE_PORT:-80}"
}

engine_version() {
  git -C "$ENGINE_DIR" describe --tags --always 2>/dev/null || true
}

engine_page_url() {
  local remote repository version
  remote=$(git -C "$ENGINE_DIR" remote get-url origin 2>/dev/null) || return 0
  [[ $remote == *github* ]] || return 0
  repository=$(sed -nE 's#^.*[:/]([^/:]+/[^/:]+)$#\1#p' <<< "${remote%.git}")
  [ -n "$repository" ] || return 0
  version=$(engine_version)
  if [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "https://github.com/$repository/releases/tag/$version"
  else
    echo "https://github.com/$repository/commit/$(git -C "$ENGINE_DIR" rev-parse HEAD)"
  fi
}

homepage_images_signature() {
  local images="$ENGINE_DIR/.homepage-images"
  [ -d "$images" ] || return 0
  (cd "$images" && find . -type f -exec cksum {} + | sort)
}

homepage_running() {
  [ "$(docker inspect -f '{{.State.Running}}' homepage 2>/dev/null)" = true ]
}

render_homepage() {
  local images_before
  images_before=$(homepage_images_signature)
  local role=${MACHINE_ROLE:-$(machine_role)}
  HOMEPAGE_HOST=$(network_name) HOMEPAGE_ENGINE_VERSION=$(engine_version) HOMEPAGE_ENGINE_URL=$(engine_page_url) \
    HOMEPAGE_HEALTHCHECK_BACKUP=$(healthcheck_slug backup) HOMEPAGE_HEALTHCHECK_VERIFY=$(healthcheck_slug verify) \
    HOMEPAGE_HEALTHCHECK_UPDATE=$(MACHINE_ROLE=$role healthcheck_slug update) \
    python3 "$(dirname "${BASH_SOURCE[0]}")/homepage.py" render "$ENGINE_DIR/.homepage"
  homepage_running || return 0
  if [ "$(homepage_images_signature)" != "$images_before" ]; then
    docker restart homepage >/dev/null
  else
    curl -fsS -o /dev/null "http://localhost:$(homepage_port)/api/revalidate" || true
  fi
}

stack_profiles() {
  local profiles
  profiles=$(printf '%s,%s' "${1:-}" "$(optional_services_pinned)")
  profiles=${profiles#,}
  echo "${profiles%,}"
}

stack_compose() {
  # shellcheck disable=SC2046
  COMPOSE_PROFILES=$(stack_profiles "${COMPOSE_PROFILES:-}") docker compose --project-name media-server --project-directory "$ENGINE_DIR" \
    --env-file "$ENGINE_DIR/.env" $(compose_files docker-compose.yml images.yml) "$@"
}

stack_compose_with_wiring() {
  COMPOSE_PROFILES=wiring stack_compose "$@"
}

monitoring_compose() {
  # shellcheck disable=SC2046
  docker compose --project-name monitoring --project-directory "$ENGINE_DIR" \
    --env-file "$ENGINE_DIR/.env" $(compose_files docker-compose.monitoring.yml images.monitoring.yml) "$@"
}

machine_role() {
  if [ -e "$DATA_DIR/.backup-main" ]; then echo main; else echo secondary; fi
}

healthcheck_slug() {
  if [ "$1" = update ] && [ "${MACHINE_ROLE:-main}" = secondary ]; then
    echo "$INSTALLATION_NAME-$1-$(hostname -s)"
  else
    echo "$INSTALLATION_NAME-$1"
  fi
}

healthcheck_url() {
  [ -n "${HEALTHCHECKS_PING_KEY:-}" ] || return 0
  echo "https://hc-ping.com/$HEALTHCHECKS_PING_KEY/$(healthcheck_slug "$1")"
}

ping_healthcheck() {
  local url
  url=$(healthcheck_url "$1")
  if [ -z "$url" ]; then
    echo "no healthchecks ping key configured; not reporting $1${2:-}" >&2
    return 0
  fi
  curl -fsS -m 10 --retry 3 -o /dev/null "$url${2:-}?create=1" || true
}

generate_secret() {
  openssl rand -base64 "$1" | tr '+/' '-_' | tr -d '=\n'
}

mask() {
  if [ -n "$1" ]; then printf 'set, ends …%s' "${1: -3}"; else printf 'not set'; fi
}

read_answer() {
  local _prompt_typed=""
  if [ -t 0 ] && [ "${2:-}" = secret ]; then
    IFS= read -rs _prompt_typed
  elif ! IFS= read -r _prompt_typed && [ -z "$_prompt_typed" ]; then
    export PROMPT_INPUT_ENDED=1
  fi
  [ -t 0 ] && [ "${2:-}" != secret ] || echo >&2
  printf -v "$1" '%s' "$_prompt_typed"
}

ask() {
  local _prompt_answer
  printf '%s [%s]: ' "$2" "${3:-}" >&2
  read_answer _prompt_answer
  printf -v "$1" '%s' "${_prompt_answer:-${3:-}}"
}

ask_secret() {
  local _prompt_answer
  printf '%s [%s]: ' "$2" "$(mask "${3:-}")" >&2
  read_answer _prompt_answer secret
  printf -v "$1" '%s' "${_prompt_answer:-${3:-}}"
}

ask_password() {
  local _prompt_answer
  if [ -n "${3:-}" ]; then
    printf '%s [%s]: ' "$2" "$(mask "$3")" >&2
  else
    printf '%s [Enter generates one]: ' "$2" >&2
  fi
  read_answer _prompt_answer secret
  if [ -n "$_prompt_answer" ]; then
    printf -v "$1" '%s' "$_prompt_answer"
  elif [ -n "${3:-}" ]; then
    printf -v "$1" '%s' "$3"
  else
    printf -v "$1" '%s' "$(generate_secret 24)"
  fi
}

installation_snapshot_filter() {
  local count
  count=$(restic snapshots --no-lock --host "$INSTALLATION_NAME" --json 2>/dev/null |
    python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
  if [ "${count:-0}" -gt 0 ]; then echo "--host $INSTALLATION_NAME"; fi
}

run_from_installation_directory() {
  local name=$1 script=$2 install_dir engine
  install_dir=${INSTALL_DIR:-$HOME/$name}
  engine="$install_dir/engine"
  if [ -d "$engine" ] && [ "$(cd "$engine" && pwd -P)" = "$(cd "$ENGINE_DIR" && pwd -P)" ]; then
    return 0
  fi
  [ ! -e "$engine" ] || die "$engine already exists; run make from $engine instead"
  local created_install_dir=""
  [ -e "$install_dir" ] || created_install_dir=$install_dir
  mkdir -p "$install_dir"
  git clone -q "$ENGINE_DIR" "$engine"
  if ! git -C "$ENGINE_DIR" symbolic-ref -q HEAD >/dev/null; then
    git -C "$engine" checkout -q "$(git -C "$ENGINE_DIR" rev-parse HEAD)"
  fi
  git -C "$engine" remote set-url origin "$(git -C "$ENGINE_DIR" remote get-url origin)"
  echo "Installing $name in $install_dir: engine, config and data side by side." >&2
  ENGINE_DIR=$engine CONFIG_DIR=$install_dir/config DATA_DIR=$install_dir/data INSTALL_DIR=$install_dir \
    CREATED_INSTALL_DIR=$created_install_dir exec "$engine/scripts/$(basename "$script")" "$name"
}

valid_installation_name() {
  [[ $1 =~ ^[a-z0-9][a-z0-9-]{0,39}$ ]]
}

require_valid_installation_name() {
  valid_installation_name "$1" ||
    die "installation names are lowercase letters, digits and dashes, up to 40 characters, starting with a letter or digit; got '$1'"
}

network_name() {
  if [ -n "${MEDIA_SERVER_HOST:-}" ]; then
    echo "$MEDIA_SERVER_HOST"
  elif [ "$(uname -s)" = Darwin ]; then
    echo "$(scutil --get LocalHostName).local"
  else
    echo "$(hostname -s).local"
  fi
}

systemd_running() {
  [ -d "${SYSTEMD_RUNTIME_DIR:-/run/systemd/system}" ] && command -v systemctl >/dev/null
}

readonly NOT_THE_MAIN=1
readonly RESTIC_LOCKED=11

explain_main_check() {
  if [ "$1" -eq "$NOT_THE_MAIN" ]; then
    echo "another machine is $INSTALLATION_NAME's main"
  else
    echo "cannot read the backup repository to tell which machine is $INSTALLATION_NAME's main"
  fi
}

backup_lock() {
  echo "$DATA_DIR/.backup.lock"
}

lock_file_descriptor() {
  python3 -c 'import fcntl, sys; fcntl.flock(int(sys.argv[1]), fcntl.LOCK_EX | fcntl.LOCK_NB)' "$1" 2>/dev/null
}

running_backup_pid() {
  local lock
  lock=$(backup_lock)
  [ -f "$lock" ] || return 0
  python3 - "$lock" <<'EOF'
import fcntl, sys
with open(sys.argv[1]) as lock:
    try:
        fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
    except OSError:
        print(lock.read().strip() or "unknown")
EOF
}

describe_restic_locks() {
  local id
  for id in $(restic list locks --no-lock 2>/dev/null); do
    restic cat lock "$id" --no-lock 2>/dev/null | python3 -c '
import json, sys
lock = json.load(sys.stdin)
print("  {} lock from {} (process {}) since {}".format(
    "exclusive" if lock.get("exclusive") else "shared",
    lock.get("hostname", "an unknown host"), lock.get("pid", "?"), lock.get("time", "?")))
' 2>/dev/null || true
  done
}

restic_explaining_locks() {
  local status=0
  restic "$@" || status=$?
  if [ "$status" -eq "$RESTIC_LOCKED" ]; then
    {
      echo "restic gave up waiting for a lock on the backup repository. Locks held:"
      describe_restic_locks
      echo "If none of those machines is running restic now, remove every lock with: make unlock-backup ALL=1"
    } >&2
  fi
  return "$status"
}
