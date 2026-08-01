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

# golang-migrate registers its database drivers behind build tags, so `go tool
# migrate` — which builds without them — fails with "unknown driver postgres".
# Found the first time these migrations were actually run, at Phase 8; every
# target below had been wrong since Phase 0 and nothing had exercised them.
MIGRATE = go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate

.PHONY: migrate-up migrate-down migrate-status migrate-new
migrate-up: ## Apply all pending migrations
	cd $(API_DIR) && $(MIGRATE) -path migrations -database "$(DB_URL)" up

migrate-down: ## Roll back one migration
	cd $(API_DIR) && $(MIGRATE) -path migrations -database "$(DB_URL)" down 1

migrate-status: ## Show the current schema version
	cd $(API_DIR) && $(MIGRATE) -path migrations -database "$(DB_URL)" version

migrate-new: ## Create a migration pair: make migrate-new name=add_reels
	@test -n "$(name)" || (echo "usage: make migrate-new name=add_reels" && exit 1)
	cd $(API_DIR) && go tool migrate create -ext sql -dir migrations -seq $(name)

# --------------------------------------------------- analytics warehouse --

# ClickHouse is deliberately outside the Postgres migration chain. The two
# stores fail independently, and a schema change to the warehouse must never be
# able to block a deploy of the transactional core.
.PHONY: clickhouse-up clickhouse-schema opensearch-mapping
clickhouse-up: ## Start ClickHouse (profile: analytics)
	docker compose --profile analytics up -d clickhouse

clickhouse-schema: ## Apply deploy/clickhouse/schema.sql
	docker compose exec -T clickhouse clickhouse-client --multiquery < deploy/clickhouse/schema.sql

opensearch-mapping: ## Create the reels index with its explicit mapping
	curl -sS -X PUT "$${OPENSEARCH_URL:-http://localhost:9200}/$${OPENSEARCH_INDEX:-reels}" \
		-H 'Content-Type: application/json' \
		--data-binary @deploy/opensearch/reels-mapping.json

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

.PHONY: run-api run-worker run-feed
run-api: ## Run the API server
	cd $(API_DIR) && go run -ldflags "-X main.version=$(GIT_SHA)" ./cmd/api

run-worker: ## Run the background worker
	cd $(API_DIR) && go run -ldflags "-X main.version=$(GIT_SHA)" ./cmd/worker

run-feed: ## Run the feed read-path service (:8081)
	cd $(API_DIR) && go run -ldflags "-X main.version=$(GIT_SHA)" ./cmd/feed

.PHONY: dev-web dev-mobile
dev-web: ## Run the Next.js web app
	npm run dev:web

dev-mobile: ## Build and install the Android app (mobile is Kotlin Multiplatform)
	cd apps/mobile-kmm && ./gradlew :androidApp:installDebug

.PHONY: test-mobile
test-mobile: check-interop parse-swift ## Compile and test the mobile module for both platforms
	#
	# The iOS *Kotlin* target is here deliberately, and it is not decorative:
	# Kotlin/Native compiles Apple targets on any host — only linking the
	# framework needs a Mac — so iosMain can and should be checked here. It was
	# not, and four compile errors accumulated in it: a Security API compared
	# across signed and unsigned, an NSDictionary factory that does not exist in
	# the bindings, a CMTime passed by value where a CValue was wanted, and a
	# method read off the wrong class. Every one would have blocked the first
	# iOS build.
	#
	# The Swift is covered as far as it can be without Xcode, by two targets
	# above rather than by nothing: parse-swift rejects anything the grammar
	# rejects, check-interop rejects a Kotlin symbol or argument label that
	# does not exist. What is left for a Mac is type checking and SwiftUI
	# semantics — which is where it belongs, and is no longer the first place
	# a missing brace gets noticed.
	cd apps/mobile-kmm && ./gradlew :shared:testDebugUnitTest \
		:shared:compileKotlinIosSimulatorArm64 :androidApp:assembleDebug

# ------------------------------------------------------------ verification --

# `test` deliberately excludes test-mobile.
#
# The KMM tests need a JDK and an Android SDK, and making the default target
# require both means a backend contributor cannot run `make test`.
#
# This comment used to end "the 38 shared-logic tests do not run unless CI calls
# test-mobile explicitly. It should." It did not, and the bill came due: four
# compile errors accumulated in iosMain for want of anyone running the compiler.
# CI now has `mobile` and `ios` jobs covering the shared tests, the Android
# build, the Swift/Kotlin boundary and — on a macOS runner, which is the only
# place it can happen — a real `swiftc -typecheck` of every SwiftUI file against
# the linked framework. Locally this target is still the way to run it.
.PHONY: test test-go test-ts test-all lint lint-go lint-ts build
test: test-go test-ts ## Run backend and TypeScript tests

test-all: test-go test-ts test-mobile ## Everything, including the KMM shared tests

# The Phase 10 load tests. Two gates rather than one, deliberately: the build
# tag keeps them out of `make test` (they need a database and take minutes), and
# the separate LOADTEST_DATABASE_URL keeps them from running against whatever
# DATABASE_URL happens to be set to — capacity_test.go writes to
# capacity_buckets, and running it against a shared environment would make for a
# very confusing afternoon.
.PHONY: loadtest
loadtest: ## Run the capacity and feed load tests (needs infra-up + migrate-up)
	cd $(API_DIR) && LOADTEST_DATABASE_URL="$(DB_URL)" go test -tags loadtest -v ./loadtest/...

test-go: ## Run Go tests with the race detector
	cd $(API_DIR) && go test -race -count=1 ./...

test-ts: ## Run TypeScript typechecking
	npm run typecheck

lint: lint-go lint-ts ## Lint everything

lint-go:
	cd $(API_DIR) && go vet ./... && gofmt -l -d .

lint-ts:
	npm run lint

build: ## Build API, worker and feed binaries
	cd $(API_DIR) && go build -ldflags "-X main.version=$(GIT_SHA)" -o bin/api ./cmd/api
	cd $(API_DIR) && go build -ldflags "-X main.version=$(GIT_SHA)" -o bin/worker ./cmd/worker
	cd $(API_DIR) && go build -ldflags "-X main.version=$(GIT_SHA)" -o bin/feed ./cmd/feed

.PHONY: ios-strings
## Regenerate the iOS .strings tables from the Android string resources.
##
## Run after touching either values/strings.xml or values-ar/strings.xml. The
## script warns when the two languages disagree in count, which is the shape
## every missing translation takes.
ios-strings:
	bash apps/mobile-kmm/tools/gen-ios-strings.sh

.PHONY: check-interop
## Validate the Swift↔Kotlin boundary without a Swift compiler.
##
## Roughly 2,500 lines of SwiftUI go unverified between Mac builds, and the
## errors they accumulate are overwhelmingly one kind: a reference to a Kotlin
## symbol that does not exist. That class is mechanically checkable, so it is
## checked here rather than waiting for someone with Xcode.
##
## A finding is a definite problem. A clean run is NOT a promise that the Swift
## compiles — this is not a type checker and does not model generics, argument
## types or SwiftUI. See tools/interopcheck for the full boundary.
check-interop:
	cd apps/mobile-kmm/tools/interopcheck && go build -o ic.exe .
	cd apps/mobile-kmm && ./tools/interopcheck/ic.exe .

.PHONY: parse-swift
## Syntax-check the iOS Swift without Xcode.
##
## `swiftc -parse` never resolves an import, so SwiftUI and the Shared
## framework not existing off a Mac costs nothing. It catches what the grammar
## rejects and nothing else — not a type, not a SwiftUI misuse. See the header
## of tools/parse-swift.sh for the exact boundary.
##
## Skips with status 0 where there is no toolchain, which is most machines.
parse-swift:
	bash apps/mobile-kmm/tools/parse-swift.sh
