# Marketplace

Video-first local discovery for the GCC. A feed is the front door; services, meal
plans and vehicles are actionable outcomes behind it.

Planning documents live in [`docs/`](./docs/README.md) — **start there**, and read
`docs/README.md` first: it holds the phase-number mapping and the open decisions
that are currently blocking work.

---

## Layout

```
api/                    OpenAPI 3.1 contract + codegen configs   ← source of truth
services/api/           Go modular monolith (API + worker)
apps/web/               Next.js 15, App Router, locale-segmented
apps/mobile/            Expo + expo-router
packages/api-client/    Generated TS client, shared by web and mobile
packages/tsconfig/      Shared TypeScript config
scripts/                Repo tooling
docs/                   Planning and design
```

`services/` holds backend deployables, `apps/` holds frontends, `packages/` holds
shared code. The Go service is a **modular monolith** — module boundaries are
enforced by package structure, not by network calls. The one split the plan calls
for is the feed read path (`PLAN-VIDEO-FIRST` §3), and it arrives in Phase 2 as
`cmd/feed`, not as a premature extraction of anything that exists today.

---

## Getting started

Requires **Go 1.24+**, **Node 22+**, and **Docker**.

```bash
cp .env.example .env
make setup          # npm install, go mod tidy, docker compose up, migrate
make run-api        # :8080
make dev-web        # :3000
```

Without `make` (Windows without Git Bash), each target in the `Makefile` is a
single command you can run directly.

Verify:

```bash
curl localhost:8080/healthz
curl localhost:8080/readyz
```

---

## The contract is the source of truth

`api/openapi.yaml` generates both the Go server boundary and the TypeScript
client. Nothing under `services/api/internal/gen/` or
`packages/api-client/src/gen/` is hand-edited, and CI fails if regenerating
produces a diff.

```bash
make gen            # regenerate everything
make gen-check      # fail if generated code is stale
```

The workflow for an API change is: edit the spec → `make gen` → implement the
handler the generated interface now requires → commit the generated files with
the change.

---

## What exists, and what does not

**Working:** config loading with eager validation, structured logging, the HTTP
middleware chain (request ID, recovery, access log, locale, timeout, CORS),
health and readiness endpoints, the Postgres pool and transaction helper, both
architectural seams, the strategy registry, the money helpers, and 10 migrations
covering the Phase 1 transactional core.

**Not yet written** — the Phase 1 work this scaffold exists to hold:

- The laundry vertical's four strategy implementations
- Auth handlers (OTP issue/verify, session rotation)
- The order → capacity reservation → quote → payment flow
- The `IdempotencyStore` implementation backing the middleware
- Outbox relay and River job wiring (the worker currently sweeps on a ticker)
- Anything under `internal/gen/` — run `make gen` to produce it

---

## Two seams, and why they matter

Both are in `services/api/internal/seam/`. They are the load-bearing
abstractions; if either leaks, adding the next vertical stops being cheap.

**`fulfillment`** — downstream. A fulfillment is the atomic unit of physical
work: someone goes somewhere at a time to do a thing. Laundry produces two
(pickup, delivery); a meal day produces one. Dispatch, routing and the driver app
read this shape and never learn what a laundry bag or a meal box is.

**`action`** — upstream, and the exact mirror. Everything the feed knows about
commerce is `(kind, target_type, target_id)`. Adding a vertical means registering
an action kind and its handler — never touching feed, ranking or moderation code.

**`servicetype`** — the registry that binds them. A `service_types` row plus four
Go interfaces *is* a vertical. If adding one requires editing ordering, pricing,
capacity or logistics code, the abstraction has leaked and that is worth stopping
for.

---

## Decisions encoded in the schema

Worth knowing before changing anything in `services/api/migrations/`:

| Decision | Where | Why |
|---|---|---|
| Money is `bigint` minor units + currency | everywhere | No float touches a price. Ever. |
| Capacity keys on **branch**, not vendor | `000005` | A chain's facilities have independent throughput |
| `CHECK (consumed + reserved <= limit)` | `000005` | Overbooking impossible even if every caller is buggy |
| Reservations have a TTL | `000005` | Abandoned checkouts must return inventory unattended |
| Order lines snapshot name and price | `000006` | A vendor's price change must not rewrite history |
| Fulfillments snapshot the address | `000007` | Editing a saved address must not redirect a driver en route |
| Price is a state machine | `000008` | Laundry-by-weight `adjusted` is normal, not an exception |
| VAT is tax-**inclusive**, extracted from total | `000008` | Getting this backwards misprices everything by the VAT rate |
| Ledger entries immutable by trigger | `000009` | Must survive an ops engineer in `psql` during an incident |
| Balance check is `DEFERRABLE INITIALLY DEFERRED` | `000009` | Entries insert one row at a time; a per-statement check rejects every valid write |
| Outbox written in the same tx as the state change | `000010` | Closes the dual-write gap |
| Idempotency is a table, not a cache | `000010` | The guarantee has to survive an eviction |

Arabic and RTL are carried from Phase 0, not retrofitted: locale is a route
segment on web, `I18nManager.forceRTL` runs at module scope on mobile, and CSS
uses logical properties throughout. Adding RTL later means re-auditing every
component ever built assuming left-to-right, which is the actual reason RTL
projects get indefinitely postponed.

---

## Verification status

**Verified locally** (Go 1.26.5):

- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l .` — clean
- `go test ./...` — passing (`money`, `ledger`)
- `redocly lint api/openapi.yaml` — valid; 10 warnings, all cosmetic
- All JSON and YAML files parse

**Not verified** — no Docker daemon was available:

- **Migrations have never been applied against PostGIS.** This is the big one:
  11 migrations including PL/pgSQL functions and a deferred constraint trigger,
  none of which the Go compiler can check. Expect to fix things here.
- Every SQL string in the repositories is unexecuted, so column-name and
  parameter-order mistakes are still live.
- `go test -race` could not run — the toolchain has `CGO_ENABLED=0` and the race
  detector requires cgo. CI runs it on Linux, where it works.
- No `npm install` has run, so the TypeScript dependency versions are unresolved.

Start with `docker compose up -d && make migrate-up`; that is where the
remaining errors are.
