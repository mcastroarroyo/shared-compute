# shared-compute — developer entrypoints
.DEFAULT_GOAL := help
SHELL := /bin/bash

COMPOSE := docker compose -f infra/docker-compose.yml

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: dev
dev: ## Bring up the local stack (coordinator + postgres + object-store stub)
	$(COMPOSE) up --build

.PHONY: dev-down
dev-down: ## Tear down the local stack
	$(COMPOSE) down -v

.PHONY: build
build: build-coordinator build-provider build-modelctl ## Build all local binaries

.PHONY: build-coordinator
build-coordinator: ## Build the Go coordinator
	cd coordinator && go build -o bin/coordinator ./cmd/coordinator

.PHONY: build-provider
build-provider: ## Build the Rust provider workspace
	cd provider-core && cargo build

.PHONY: build-modelctl
build-modelctl: ## Build the model-registry CLI
	cd model-registry && cargo build

.PHONY: test
test: ## Run unit tests (Go + Rust)
	cd coordinator && go test ./...
	cd provider-core && cargo test

.PHONY: e2e
e2e: ## Run the end-to-end encrypted round-trip test
	./e2e-tests/run.sh

.PHONY: fmt
fmt: ## Format all code
	cd coordinator && gofmt -w .
	cd provider-core && cargo fmt
	cd model-registry && cargo fmt

.PHONY: lint
lint: lint-nolog schemas ## Lint all code
	cd coordinator && go vet ./...
	cd provider-core && cargo clippy --all-targets -- -D warnings
	cd model-registry && cargo clippy --all-targets -- -D warnings

.PHONY: lint-nolog
lint-nolog: ## Fail if prompt/completion content looks like it is being logged
	@./scripts/check-no-prompt-logging.sh

.PHONY: schemas
schemas: ## Validate protocol JSON Schemas
	@./scripts/validate-schemas.sh

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf coordinator/bin provider-core/target e2e-tests/tmp
