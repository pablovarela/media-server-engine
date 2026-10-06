.PHONY: help test test-scripts lint go-build go-test go-lint go-mocks release-snapshot

SHELL := /bin/bash

help: ## list the targets
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

test: test-scripts go-test lint ## run the script tests, the Go tests and the linters (needs bats-core, go and shellcheck)

TEST_JOBS ?= $(shell command -v parallel >/dev/null && getconf _NPROCESSORS_ONLN)

test-scripts: ## run the script tests, in parallel when GNU parallel is installed (TEST_JOBS=1 runs them one at a time, BATS_FLAGS passes options to bats)
	@command -v bats >/dev/null || { echo "bats missing: brew install bats-core" >&2; exit 1; }
	@BATS_TEST_TIMEOUT=$${BATS_TEST_TIMEOUT:-120} bats $(if $(TEST_JOBS),--jobs $(TEST_JOBS)) $(BATS_FLAGS) tests/

GO_TOOL = go tool -modfile=tools/go.mod
GOLANGCI_LINT ?= $(GO_TOOL) golangci-lint
GORELEASER ?= $(GO_TOOL) goreleaser

go-build: ## build mse for this machine into dist/mse
	@command -v go >/dev/null || { echo "go missing: brew install go" >&2; exit 1; }
	@go build -o dist/mse .

go-test: ## run the Go tests with coverage into coverage.out (GO_TEST_FLAGS passes options to gotestsum)
	@command -v go >/dev/null || { echo "go missing: brew install go" >&2; exit 1; }
	@$(GO_TOOL) gotestsum $(GO_TEST_FLAGS) -- -coverprofile=coverage.out -coverpkg=./cmd/...,./internal/... . ./cmd/... ./internal/...

go-lint: ## lint the Go code with golangci-lint (GOLANGCI_LINT runs another golangci-lint binary)
	@command -v go >/dev/null || { echo "go missing: brew install go" >&2; exit 1; }
	@$(GOLANGCI_LINT) run

go-mocks: ## regenerate the Go test mocks from .mockery.yml
	@command -v go >/dev/null || { echo "go missing: brew install go" >&2; exit 1; }
	@$(GO_TOOL) mockery

release-snapshot: ## build every release archive into dist/ without publishing (GORELEASER runs another goreleaser binary)
	@command -v go >/dev/null || { echo "go missing: brew install go" >&2; exit 1; }
	@$(GORELEASER) check
	@$(GORELEASER) release --snapshot --clean

lint: ## shellcheck every script and lint the Go code, reporting both before failing
	@command -v shellcheck >/dev/null || { echo "shellcheck missing: brew install shellcheck" >&2; exit 1; }
	@status=0; \
	shellcheck -x scripts/*.sh install.sh || status=1; \
	$(MAKE) --no-print-directory go-lint || status=1; \
	exit $$status
