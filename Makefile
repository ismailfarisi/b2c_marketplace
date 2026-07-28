.DEFAULT_GOAL := help
SHELL := /bin/bash

API_DIR := services/api
DB_URL  ?= postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

# Windows users: this Makefile assumes a POSIX shell (Git Bash / WSL). Without
# make, each recipe below is a single command you can run directly.

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- bootstrap --

.PHONY: setup
setup: ## Install dependencies and start local infrastructure
	npm install
	cd $(API_DIR) && go mod tidy
	docker compose up -d
	@echo "waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U marketplace >/dev/null 2>&1; do sleep 1; done
	$(MAKE) migrate-up
	@echo "ready. cp .env.example .env, then: make run-api"

.PHONY: infra-up infra-down infra-reset
infra-up: ## Start postgres, redis, minio, mailpit
	docker compose up -d

infra-down: ## Stop local infrastructure
	docker compose down

infra-reset: ## Destroy local data and start clean
	docker compose down -v
	docker compose up -d

# --------------------------------------------------------------- migrations --

.PHONY: migrate-up migrate-down migrate-status migrate-new
migrate-up: ## Apply all pending migrations
	cd $(API_DIR) && go tool migrate -path migrations -database "$(DB_URL)" up

migrate-down: ## Roll back one migration
	cd $(API_DIR) && go tool migrate -path migrations -database "$(DB_URL)" down 1

migrate-status: ## Show the current schema version
	cd $(API_DIR) && go tool migrate -path migrations -database "$(DB_URL)" version

migrate-new: ## Create a migration pair: make migrate-new name=add_reels
	@test -n "$(name)" || (echo "usage: make migrate-new name=add_reels" && exit 1)
	cd $(API_DIR) && go tool migrate create -ext sql -dir migrations -seq $(name)

# ------------------------------------------------------------------ codegen --

.PHONY: gen gen-go gen-ts gen-sqlc gen-check
gen: gen-go gen-ts gen-sqlc ## Regenerate everything from the contract and schema

gen-go: ## Generate the Go server boundary from api/openapi.yaml
	go tool -C $(API_DIR) oapi-codegen -config ../../api/oapi-codegen.yaml ../../api/openapi.yaml

gen-ts: ## Generate the TypeScript client from api/openapi.yaml
	npx orval --config api/orval.config.ts

gen-sqlc: ## Generate typed queries from migrations + internal/db/queries
	cd $(API_DIR) && go tool sqlc generate

gen-check: ## Fail if generated code is stale (CI runs this)
	node scripts/check-generated.mjs

# --------------------------------------------------------------------- dev --

.PHONY: run-api run-worker
run-api: ## Run the API server
	cd $(API_DIR) && go run -ldflags "-X main.version=$(GIT_SHA)" ./cmd/api

run-worker: ## Run the background worker
	cd $(API_DIR) && go run -ldflags "-X main.version=$(GIT_SHA)" ./cmd/worker

.PHONY: dev-web dev-mobile
dev-web: ## Run the Next.js web app
	npm run dev:web

dev-mobile: ## Run the Expo mobile app
	npm run dev:mobile

# ------------------------------------------------------------ verification --

.PHONY: test test-go test-ts lint lint-go lint-ts build
test: test-go test-ts ## Run all tests

test-go: ## Run Go tests with the race detector
	cd $(API_DIR) && go test -race -count=1 ./...

test-ts: ## Run TypeScript typechecking
	npm run typecheck

lint: lint-go lint-ts ## Lint everything

lint-go:
	cd $(API_DIR) && go vet ./... && gofmt -l -d .

lint-ts:
	npm run lint

build: ## Build API and worker binaries
	cd $(API_DIR) && go build -ldflags "-X main.version=$(GIT_SHA)" -o bin/api ./cmd/api
	cd $(API_DIR) && go build -ldflags "-X main.version=$(GIT_SHA)" -o bin/worker ./cmd/worker
