SHELL := /bin/bash
GO := /usr/local/go/bin/go
BIN_DIR := bin

.PHONY: all build test clean up down restart logs ps run-tui

all: build

build:
	@mkdir -p $(BIN_DIR)
	@echo "🔨 Building api-gateway..."
	@cd api-gateway && $(GO) build -ldflags="-w -s" -o ../$(BIN_DIR)/api-gateway .
	@echo "🔨 Building auth-service..."
	@cd auth-service && $(GO) build -ldflags="-w -s" -o ../$(BIN_DIR)/auth-service .
	@echo "🔨 Building telemetry-service..."
	@cd telemetry-service && $(GO) build -ldflags="-w -s" -o ../$(BIN_DIR)/telemetry-service .
	@echo "🔨 Building tui-client..."
	@cd tui-client && $(GO) build -ldflags="-w -s" -o ../$(BIN_DIR)/tui-client .
	@echo "✅ All GliTch binaries built successfully in $(BIN_DIR)/"

run-tui:
	@cd tui-client && $(GO) run .

up:
	docker compose up --build -d

down:
	docker compose down

restart:
	docker compose restart

logs:
	docker compose logs -f

ps:
	docker compose ps

test:
	@echo "🧪 Running tests across monorepo..."
	@cd api-gateway && $(GO) test -v ./...
	@cd auth-service && $(GO) test -v ./...
	@cd telemetry-service && $(GO) test -v ./...
	@cd tui-client && $(GO) test -v ./...

clean:
	@rm -rf $(BIN_DIR)
	@echo "🧹 Clean complete."
