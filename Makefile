.PHONY: help urls logins create-installation join-installation setup-machine bootstrap configure update install-update-timer claim-backup-main check-tools test restore backup-now verify-backup-now install-backup-timers install-download-cleanup-timer media-start media-stop media-status monitoring-start monitoring-stop monitoring-status

SHELL := /bin/bash
CONFIG_DIR ?= $(CURDIR)/../config
export CONFIG_DIR
WITH_LIB = source scripts/lib.sh &&
export SOPS_AGE_KEY_FILE ?= $(HOME)/.config/sops/age/keys.txt

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

create-installation: ## create a new installation in ~/NAME (engine, config, data), asking for its settings, and set up this machine: NAME=<name>
	@scripts/create-installation.sh $(NAME)

join-installation: ## add this machine to an installation whose config is on GitHub, in ~/NAME: NAME=<installation name>
	@scripts/join-installation.sh $(NAME)

setup-machine: ## finish setting up this machine for its installation (checks, main question, update, timers)
	@scripts/setup-machine.sh

bootstrap: ## install sops, age and restic (Homebrew on macOS, pinned binaries plus Docker on Debian)
	@scripts/bootstrap.sh

restore: check-tools ## restore volumes/ from the latest backup (ARGS=--overwrite replaces existing data)
	@sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" "scripts/restore.sh $(ARGS)"

update: check-tools ## pull the config repo, switch to its engine version, bring the stack up and wire the apps
	@scripts/update.sh

install-update-timer: check-tools ## schedule make update daily at 05:00, after the backup (systemd)
	@scripts/install-timers.sh media-update

configure: ## set or change this installation's settings and secrets interactively (ROTATE=sonarr regenerates one internal key)
	@scripts/configure.sh $(if $(ROTATE),--rotate $(ROTATE))

check-tools: ## check that every tool the scripts need is installed and the age key works
	@scripts/check-tools.sh

test: ## run the script tests and shellcheck (needs bats-core and shellcheck)
	@command -v bats >/dev/null || { echo "bats missing: brew install bats-core" >&2; exit 1; }
	@command -v shellcheck >/dev/null || { echo "shellcheck missing: brew install shellcheck" >&2; exit 1; }
	@bats tests/
	@shellcheck -x scripts/*.sh scripts/wire/*.sh diagnose.sh

backup-now: ## back up now (stops the apps for a few minutes; only on the installation's main)
	@sops exec-env "$(CONFIG_DIR)/secrets/healthchecks.sops.env" 'sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" scripts/backup.sh'

verify-backup-now: ## check the backups now: restic check, and a test restore of the latest snapshot
	@sops exec-env "$(CONFIG_DIR)/secrets/healthchecks.sops.env" 'sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" scripts/verify-backup.sh'

install-backup-timers: check-tools ## schedule make backup-now daily and make verify-backup-now weekly (systemd; only on the installation's main)
	@sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" 'scripts/install-timers.sh media-backup media-verify'

claim-backup-main: check-tools ## make this machine the installation's main: runs one backup tagged with this machine
	@sops exec-env "$(CONFIG_DIR)/secrets/healthchecks.sops.env" 'sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" scripts/claim-backup-main.sh'

install-download-cleanup-timer: check-tools ## schedule the removal of downloads Sonarr or Radarr flag as executables, every 15 minutes (systemd)
	@scripts/install-timers.sh media-download-cleanup

media-start: ## start the media server stack
	@echo "==> starting media server stack..."
	@$(WITH_LIB) stack_compose up -d

media-stop: ## stop the media server stack
	@echo "==> stopping media server stack..."
	@$(WITH_LIB) stack_compose down

urls: ## list the address of every app of this installation
	@scripts/apps.sh urls

logins: ## show the app logins, passwords included, on this terminal
	@scripts/apps.sh logins

media-status: ## show status of the media server stack
	@$(WITH_LIB) stack_compose ps

monitoring-start: ## start the monitoring stack (prometheus, grafana, cadvisor, node-exporter)
	@echo "==> starting monitoring stack..."
	@docker volume create media-server_prometheus >/dev/null
	@docker volume create media-server_grafana >/dev/null
	@$(WITH_LIB) monitoring_compose up -d
	@scripts/prune-stack-images.sh

monitoring-stop: ## stop the monitoring stack
	@echo "==> stopping monitoring stack..."
	@$(WITH_LIB) monitoring_compose down

monitoring-status: ## show status of the monitoring stack
	@$(WITH_LIB) monitoring_compose ps
