module github.com/marketplace/api

go 1.24

require (
	github.com/go-chi/chi/v5 v5.2.1
	github.com/go-chi/cors v1.2.1
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.2
)

// Pinned build tools. `go tool <name>` runs these without polluting the runtime
// dependency graph and without relying on whatever happens to be on a developer's
// PATH — the generator version is part of the repo, not part of the laptop.
tool (
	github.com/golang-migrate/migrate/v4/cmd/migrate
	github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
	github.com/sqlc-dev/sqlc/cmd/sqlc
)
