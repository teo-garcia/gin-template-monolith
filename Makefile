.PHONY: help dev build start start-prod tools lint lint-check lint-sync format format-check \
        test test-integration run-integration coverage check db-migrate db-deploy \
        db-rollback db-seed db-version docker-dev docker-build clean

# Pinned so a Renovate bump to the linter is a reviewable change, not a
# surprise diff in CI.
GOLANGCI_LINT_VERSION ?= v2.11.0

BIN_DIR      := $(CURDIR)/bin
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.cache/golangci-lint
LINT_CONFIG  := .golangci.yml
LINT_OVERLAY := golangci.overrides.yml
BINARY       := $(BIN_DIR)/api

export GOLANGCI_LINT_CACHE

ENV_FILE ?= .env
# Export the env file into the recipe's environment without leaking it into
# make's own variables.
RUN_WITH_ENV = set -a; [ ! -f "$(ENV_FILE)" ] || . "./$(ENV_FILE)"; set +a;

GO_LDFLAGS := -s -w

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --- Run -------------------------------------------------------------------

dev: ## Start the API with the local env file
	@$(RUN_WITH_ENV) go run ./cmd/api

build: ## Build the production binary into ./bin
	go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BINARY) ./cmd/api

start: build ## Run the production binary
	@$(RUN_WITH_ENV) exec $(BINARY)

start-prod: start ## Alias for start

## --- Quality ---------------------------------------------------------------

$(GOLANGCI_LINT):
	GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

tools: $(GOLANGCI_LINT) ## Install pinned dev tools into ./bin

# Lint and format settings come from golangci-config-shared. The generated file
# is gitignored build output; put repo-specific rules in $(LINT_OVERLAY).
$(LINT_CONFIG): go.mod $(wildcard $(LINT_OVERLAY))
	go tool golangci-config-path --out $(LINT_CONFIG) --overlay $(LINT_OVERLAY)

lint-sync: ## Regenerate .golangci.yml from golangci-config-shared
	@rm -f $(LINT_CONFIG)
	@$(MAKE) --no-print-directory $(LINT_CONFIG)

lint: lint-check ## Alias for lint-check

lint-check: $(GOLANGCI_LINT) $(LINT_CONFIG) ## Run linters
	$(GOLANGCI_LINT) run --config $(LINT_CONFIG) ./...

format: $(GOLANGCI_LINT) $(LINT_CONFIG) ## Apply gofumpt + gci
	$(GOLANGCI_LINT) fmt --config $(LINT_CONFIG) ./...

format-check: $(GOLANGCI_LINT) $(LINT_CONFIG) ## Verify formatting without writing
	$(GOLANGCI_LINT) fmt --diff --config $(LINT_CONFIG) ./...

## --- Test ------------------------------------------------------------------

# Canonical test and coverage flags come from gotest-config-shared, so the
# runner never drifts from the portfolio contract.
GOTEST_ARGS = $(shell go tool gotest-config-path --args)

test: ## Run unit tests (no infrastructure required)
	go test -race -shuffle=on ./...

test-integration: ## Run tests that need Postgres and Redis
	@ENV_FILE=.env.test $(MAKE) --no-print-directory run-integration

run-integration:
	@$(RUN_WITH_ENV) go test -race -tags=integration ./...

# gotest-cover applies the exclusions Go cannot express, then writes the text,
# lcov, and html reporters the portfolio coverage contract requires.
coverage: ## Run tests with coverage into coverage/ (text + lcov + html)
	@mkdir -p coverage
	go test $(GOTEST_ARGS)
	go tool gotest-cover

check: lint-check format-check test coverage ## Single CI gate: lint + format + test + coverage

## --- Database --------------------------------------------------------------

db-migrate: ## Apply pending migrations locally
	@$(RUN_WITH_ENV) go run ./cmd/migrate up

db-deploy: ## Apply pending migrations (pre-deploy step; idempotent)
	@$(RUN_WITH_ENV) go run ./cmd/migrate up

db-rollback: ## Roll back all migrations (local/test only)
	@$(RUN_WITH_ENV) go run ./cmd/migrate down

db-version: ## Print the current schema version
	@$(RUN_WITH_ENV) go run ./cmd/migrate version

db-seed: ## Load deterministic sample data
	@$(RUN_WITH_ENV) go run ./cmd/seed

## --- Docker ----------------------------------------------------------------

docker-dev: ## Start the full development stack
	docker compose up --build

docker-build: ## Build the production image
	docker build -f docker/Dockerfile -t gin-template-monolith .

clean: ## Remove build artifacts
	rm -rf bin coverage $(LINT_CONFIG)
