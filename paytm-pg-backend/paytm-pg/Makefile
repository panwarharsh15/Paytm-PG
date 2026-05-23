# ─────────────────────────────────────────────────────────────────────────────
# Makefile — shortcuts for all common dev tasks
# Usage: make <target>
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: help run build docker-up docker-down test lint tidy

# Binary output
BINARY=paytm-pg
BUILD_DIR=./bin

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ─── Local Development ───────────────────────────────────────────────────────

run: ## Run the server locally (requires DB running)
	@cp -n .env.example .env 2>/dev/null || true
	go run ./cmd/server

run-watch: ## Run with hot reload (requires: go install github.com/air-verse/air@latest)
	air

build: ## Build binary
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY) ./cmd/server
	@echo "Binary built: $(BUILD_DIR)/$(BINARY)"

# ─── Docker ──────────────────────────────────────────────────────────────────

docker-up: ## Start all services (postgres, redis, api)
	docker-compose up -d
	@echo "Services running. API: http://localhost:8080"

docker-up-tools: ## Start with pgAdmin
	docker-compose --profile tools up -d

docker-up-monitoring: ## Start with Prometheus + Grafana
	docker-compose --profile monitoring up -d

docker-down: ## Stop all services
	docker-compose down

docker-down-clean: ## Stop all services and delete volumes (DESTROYS DATA)
	docker-compose down -v
	@echo "All volumes deleted"

docker-logs: ## Tail API logs
	docker-compose logs -f api

docker-build: ## Build production Docker image
	docker build -t paytm-pg:latest .
	@echo "Image built: paytm-pg:latest"

# ─── Database ────────────────────────────────────────────────────────────────

db-shell: ## Connect to postgres shell
	docker-compose exec postgres psql -U postgres -d paytm_pg

db-reset: ## Drop and recreate database (DESTROYS DATA)
	docker-compose exec postgres psql -U postgres -c "DROP DATABASE IF EXISTS paytm_pg;"
	docker-compose exec postgres psql -U postgres -c "CREATE DATABASE paytm_pg;"

# ─── Code Quality ────────────────────────────────────────────────────────────

test: ## Run all tests
	go test ./... -v -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-short: ## Run tests without integration tests
	go test ./... -short

lint: ## Run linter (requires: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

tidy: ## Tidy go modules
	go mod tidy

# ─── Security ────────────────────────────────────────────────────────────────

security-scan: ## Run security scan (requires: go install github.com/securego/gosec/v2/cmd/gosec@latest)
	gosec ./...

vuln-check: ## Check for known vulnerabilities
	go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

# ─── Generate JWT Secret ─────────────────────────────────────────────────────

gen-secret: ## Generate a JWT secret
	@openssl rand -hex 32
