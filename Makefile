.PHONY: help bootstrap check-tools test restore backup-now verify-backup-now install-backup-timers install-download-cleanup-timer media-start media-stop media-status monitoring-start monitoring-stop monitoring-status

SHELL := /bin/bash
CONFIG_DIR ?= $(CURDIR)/../config
export CONFIG_DIR
WITH_LIB = source scripts/lib.sh &&
export SOPS_AGE_KEY_FILE ?= $(HOME)/.config/sops/age/keys.txt

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

bootstrap: ## install sops, age and restic (Homebrew on macOS, pinned binaries plus Docker on Debian)
	@scripts/bootstrap.sh

restore: check-tools ## restore volumes/ from the latest backup (ARGS=--overwrite replaces existing data)
	@sops exec-env "$(CONFIG_DIR)/secrets/backup.sops.env" "scripts/restore.sh $(ARGS)"

check-tools: ## check that every tool the scripts need is installed and the age key works
	@scripts/check-tools.sh

test: ## run the script tests and shellcheck (needs bats-core and shellcheck)
	@command -v bats >/dev/null || { echo "bats missing: brew install bats-core" >&2; exit 1; }
	@command -v shellcheck >/dev/null || { echo "shellcheck missing: brew install shellcheck" >&2; exit 1; }
	@bats tests/
	@shellcheck -x scripts/*.sh diagnose.sh

backup-now: ## run the scheduled backup now (stops the stack for a few minutes; needs the timers installed)
	@command -v systemctl >/dev/null || { echo "backups run under systemd; this machine has no systemctl" >&2; exit 1; }
	@sudo systemctl start media-backup.service

verify-backup-now: ## run the scheduled backup verification now (needs the timers installed)
	@command -v systemctl >/dev/null || { echo "backups run under systemd; this machine has no systemctl" >&2; exit 1; }
	@sudo systemctl start media-verify.service

install-backup-timers: check-tools ## install and enable the nightly backup and weekly verification (systemd; run on one machine only)
	@scripts/install-timers.sh media-backup media-verify

install-download-cleanup-timer: check-tools ## remove downloads Sonarr or Radarr flag as executables, every 15 minutes (systemd)
	@scripts/install-timers.sh media-download-cleanup

media-start: ## start the media server stack
	@echo "==> starting media server stack..."
	@$(WITH_LIB) stack_compose up -d

media-stop: ## stop the media server stack
	@echo "==> stopping media server stack..."
	@$(WITH_LIB) stack_compose down

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
