APP_NAME ?= assistant
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/$(APP_NAME)
CONFIG ?= config.example.yaml
GO ?= go
PKG ?= ./...
IMAGE ?= personal-assistant:dev
COMPOSE ?= docker compose

.PHONY: help config run build test tidy fmt check docker-build docker-up docker-down docker-logs clean

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z_\/-]+:.*##/ {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

config: ## Create config.yaml from config.example.yaml if missing.
	@test -f $(CONFIG) || cp config.example.yaml $(CONFIG)

run: ## Run the service with CONFIG=config.yaml.
	$(GO) run ./cmd/assistant run -c $(CONFIG)

build: ## Build the assistant binary into bin/.
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) ./cmd/assistant

test: ## Run all Go tests.
	$(GO) test $(PKG)

tidy: ## Tidy Go module dependencies.
	$(GO) mod tidy

fmt: ## Format Go source files.
	gofmt -w cmd internal

check: fmt tidy test ## Format, tidy, and test.
	@echo "check passed"

docker-build: ## Build the Docker image.
	docker build -t $(IMAGE) .

docker-up: ## Start PostgreSQL, observability, and assistant with Docker Compose.
	$(COMPOSE) up --build

docker-down: ## Stop Docker Compose services.
	$(COMPOSE) down

docker-logs: ## Follow Docker Compose logs.
	$(COMPOSE) logs -f

clean: ## Remove local build artifacts.
	rm -rf $(BIN_DIR) coverage.out
