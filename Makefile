.PHONY: help installation test-scripts lint urls logins create-installation join-installation setup-machine bootstrap pinned-tools configure update install-update-timer claim-backup-main check-tools test restore backup-now verify-backup-now unlock-backup version homepage install-backup-timers install-download-cleanup-timer media-start media-stop media-status monitoring-start monitoring-stop monitoring-status

SHELL := /bin/bash
CONFIG_DIR ?= $(abspath $(CURDIR)/../config)
export CONFIG_DIR
WITH_LIB = export CONFIG_DIR="$(CONFIG_DIR)"; source scripts/lib.sh &&
export SOPS_AGE_KEY_FILE ?= $(HOME)/.config/sops/age/keys.txt

EVERYDAY = update configure version urls logins backup-now verify-backup-now media-start media-stop media-status
SHOW_TARGETS = awk 'BEGIN {FS = ":.*?\#\# "}; {printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2}'

help:
	@echo "Everyday:"
	@for target in $(EVERYDAY); do grep -hE "^$$target:.*## " $(MAKEFILE_LIST) | $(SHOW_TARGETS); done
	@echo "Everything else:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -v $(foreach target,$(EVERYDAY),-e '^$(target):') | sort | $(SHOW_TARGETS)

create-installation: ## create a new installation in ~/NAME (engine, config, data), asking for its settings, and set up this machine: NAME=<name>
	@scripts/engine-run create-installation $(NAME)

join-installation: ## add this machine to an installation whose config is on GitHub, in ~/NAME: NAME=<installation name>
	@scripts/engine-run join-installation $(NAME)

setup-machine: installation ## finish setting up this machine for its installation (checks, main question, update, timers)
	@scripts/engine-run setup-machine

bootstrap: ## install sops, age and restic (Homebrew on macOS, pinned binaries plus Docker on Debian)
	@scripts/bootstrap.sh

restore: installation check-tools ## restore volumes/ from the latest backup (ARGS=--overwrite replaces existing data)
	@sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" "scripts/engine-run restore $(ARGS)"

update: installation pinned-tools check-tools ## pull the config repo, switch to its engine version, bring the stack up and wire the apps
	@scripts/engine-run update

install-update-timer: installation check-tools ## schedule make update daily at 05:00, after the backup (systemd)
	@scripts/engine-run install-timers media-update

configure: ## set or change this installation's settings and secrets interactively (ROTATE=sonarr regenerates one internal key)
	@scripts/engine-run configure $(if $(ROTATE),--rotate $(ROTATE))

pinned-tools: ## install the pinned sops, age and restic where the installed version differs (Linux; Homebrew manages them on macOS)
	@scripts/bootstrap.sh --pinned-tools

version: installation ## show the engine release this installation runs and the one its config pins
	@scripts/engine-run version

homepage: installation ## redraw the landing page from the config's homepage files, without restarting anything (an open page reloads itself)
	@scripts/homepage-render.sh

check-tools: ## check that every tool the scripts need is installed and the age key works
	@scripts/check-tools.sh

installation:
	@$(WITH_LIB) require_installation

test: test-scripts test-python lint ## run the script tests, the Python tests and shellcheck (needs bats-core, uv and shellcheck)

TEST_JOBS ?= $(shell command -v parallel >/dev/null && getconf _NPROCESSORS_ONLN)

test-scripts: ## run the script tests, in parallel when GNU parallel is installed (TEST_JOBS=1 runs them one at a time, BATS_FLAGS passes options to bats)
	@command -v bats >/dev/null || { echo "bats missing: brew install bats-core" >&2; exit 1; }
	@BATS_TEST_TIMEOUT=$${BATS_TEST_TIMEOUT:-120} bats $(if $(TEST_JOBS),--jobs $(TEST_JOBS)) $(BATS_FLAGS) tests/

test-python: ## run the Python tests with pytest through uv (PYTHON=3.9 runs them on that Python, PYTEST_FLAGS passes options to pytest)
	@command -v uv >/dev/null || { echo "uv missing: brew install uv" >&2; exit 1; }
	@uv run --frozen $(if $(PYTHON),--python $(PYTHON)) pytest $(PYTEST_FLAGS)

lint: ## shellcheck every script
	@command -v shellcheck >/dev/null || { echo "shellcheck missing: brew install shellcheck" >&2; exit 1; }
	@shellcheck -x scripts/*.sh scripts/wire/*.sh diagnose.sh

backup-now: installation ## back up now (stops the apps for a few minutes; only on the installation's main)
	@sops exec-env "$(CONFIG_DIR)/secrets/healthchecks.sops.env" 'sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" scripts/engine-run backup'

verify-backup-now: installation ## check the backups now: restic check, and a test restore of the latest snapshot
	@sops exec-env "$(CONFIG_DIR)/secrets/healthchecks.sops.env" 'sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" scripts/engine-run verify-backup'

unlock-backup: installation ## remove stale locks from the backup repository and show the ones left (ALL=1 removes every lock: only when no machine is running restic)
	@sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" 'scripts/engine-run unlock-backup $(if $(ALL),--remove-all)'

install-backup-timers: installation check-tools ## schedule make backup-now daily and make verify-backup-now weekly (systemd; only on the installation's main)
	@sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" 'scripts/engine-run install-timers media-backup media-verify'

claim-backup-main: installation check-tools ## make this machine the installation's main: runs one backup tagged with this machine
	@sops exec-env "$(CONFIG_DIR)/secrets/healthchecks.sops.env" 'sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" scripts/engine-run claim-backup-main'

install-download-cleanup-timer: installation check-tools ## schedule the removal of downloads Sonarr or Radarr flag as executables, every 15 minutes (systemd)
	@scripts/engine-run install-timers media-download-cleanup

media-start: installation ## start the media server stack
	@echo "==> starting media server stack..."
	@$(WITH_LIB) stack_compose up -d

media-stop: installation ## stop the media server stack
	@echo "==> stopping media server stack..."
	@$(WITH_LIB) stack_compose down

urls: installation ## list the address of every app of this installation
	@scripts/engine-run urls

logins: installation ## show the app logins, passwords included, on this terminal
	@scripts/engine-run logins

media-status: installation ## show status of the media server stack
	@$(WITH_LIB) stack_compose ps

monitoring-start: installation ## start the monitoring stack (prometheus, grafana, cadvisor, node-exporter)
	@echo "==> starting monitoring stack..."
	@docker volume create media-server_prometheus >/dev/null
	@docker volume create media-server_grafana >/dev/null
	@$(WITH_LIB) monitoring_compose up -d
	@scripts/engine-run prune-stack-images

monitoring-stop: installation ## stop the monitoring stack
	@echo "==> stopping monitoring stack..."
	@$(WITH_LIB) monitoring_compose down

monitoring-status: installation ## show status of the monitoring stack
	@$(WITH_LIB) monitoring_compose ps
