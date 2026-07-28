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
services/api/           Go modular monolith (API + worker + feed)
apps/web/               Next.js 15, App Router, locale-segmented
apps/mobile/            Expo + expo-router
packages/api-client/    Generated TS client, shared by web and mobile
packages/tsconfig/      Shared TypeScript config
scripts/                Repo tooling
docs/                   Planning and design
```

`services/` holds backend deployables, `apps/` holds frontends, `packages/` holds
shared code. The Go service is a **modular monolith** — module boundaries are
enforced by package structure, not by network calls.

Three binaries, one module:

| Binary | Port | Why it is separate |
|---|---|---|
| `cmd/api` | 8080 | Transactional core: auth, orders, payments, reel authoring |
| `cmd/feed` | 8081 | Feed reads scale with **viewers**; orders scale with **buyers**. In a video-first product those differ by orders of magnitude, and sharing a process means autoscaling the payment path on people who are only watching |
| `cmd/worker` | — | The media pipeline and the sweepers |

`cmd/feed` owns no tables and performs no writes to `reels`. It reads the same
Postgres the API writes, and it can be deleted tomorrow by mounting `MountFeed`
in `cmd/api`. That is the test for whether a split is real or ceremonial.

---

## Getting started

Requires **Go 1.24+**, **Node 22+**, and **Docker**.

```bash
cp .env.example .env
make setup          # npm install, go mod tidy, docker compose up, migrate
make run-api        # :8080  transactional core
make run-feed       # :8081  feed read path
make run-worker     #        media pipeline + sweepers
make dev-web        # :3000
```

`ffprobe` is optional in development — `FFPROBE_BINARY=stub` in `.env.example`
skips it. Set it to a real path or drop the variable once you want the
validation gate exercised against actual files.

OpenSearch is behind a compose profile, because it is a JVM nobody needs running
to work on the order path:

```bash
docker compose --profile search up -d
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

**Phase 0 — scaffold.** Config with eager validation, structured logging, the
middleware chain (request ID, recovery, access log, locale, timeout, CORS),
health and readiness, the Postgres pool and transaction helper, both
architectural seams, the strategy registry, the money helpers.

**Phase 1 — transactional core.** OTP auth with session rotation, the laundry
vertical's four strategies, order → capacity reservation → quote → payment,
double-entry ledger, transactional outbox, idempotency store.

**Phase 2 — media and feed.** Presigned direct-to-S3 upload, the reel state
machine, the probe validation gate, transcode behind a provider seam, the
layered moderation gate with GCC thresholds and Claude text classification,
author trust tiers, the geo-filtered feed with keyset pagination, Redis counters
and seen-sets, batched event ingest, and the OpenSearch indexer.

**Not yet written:**

- `/me`, `/me/addresses`, `/vendors/{vendorId}`, `/orders` list, `/auth/logout`
- **Rekognition.** The vision moderation layers are stubs that return no labels,
  which reads as approve. `cmd/*` refuses to start in production because of it —
  a stub here would silently publish everything.
- **A real transcoder.** `media.StubProvider` synthesises renditions whose
  playlists point at nothing. Same production guard.
- River job wiring — the worker still polls on a ticker
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

Phase 2 adds three more seams, all for the same reason — every one of them is an
external service that will be swapped, and none of them should be swappable only
by rewriting a module:

- **`media.TranscodeProvider`** — Mux now, MediaConvert when delivery volume
  makes the per-minute difference matter. Completion is always asynchronous, so
  `Submit` returns a handle and never a result; modelling it as a blocking call
  works in a demo and falls over on the first 90-second source.
- **`moderation.VisionModerator` / `TextModerator`** — split because they are
  genuinely different problems. Vision is Rekognition, where a specialised model
  is both better and an order of magnitude cheaper. Text is Claude, because
  captions here are Gulf-dialect Arabic, transliterated Arabic in Latin script
  with digits for letters, or both code-switched inside one sentence — and the
  AWS-native option is effectively blind to all three.
- **`feed.SeenStore`** — Redis, with a no-op fallback. The consequence of the
  fallback is stated, not hidden: the feed repeats itself. That is a degraded
  front door, which beats a 503 one.

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
| Reels carry `bytes_ready_at` **and** `metadata_ready_at` | `000012` | The upload and the caption arrive independently and in either order |
| One `coverage` geography, resolved at publish time | `000012` | Polygon-scoped services and radius-scoped listings become one predicate and one index |
| `gist (coverage, published_at)` needs `btree_gist` | `000012` | Without it the planner fetches every geo match and sorts the lot |
| Reels are polymorphic (`author_type`) from the first migration | `000012` | Phase 6 adds user content; retrofitting polymorphism onto a live table is a migration nobody schedules |
| A takedown keeps the row | `000012` | The state and moderation history is the evidence when the decision is disputed |
| Every raw moderation label is stored | `000012` | The thresholds get retuned against the queue's overturn rate, and you cannot tune on data you discarded |

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
- `go test ./...` — passing (`money`, `ledger`, `reels`, `moderation`, `media`,
  `feed`)
- `redocly lint api/openapi.yaml` — valid; 14 warnings, all cosmetic

**Not verified** — no Docker daemon has been available at any point:

- **Migrations have never been applied against PostGIS.** Still the big one, and
  it grew: 12 migrations now, including four PL/pgSQL functions, a deferred
  constraint trigger, a `gist (coverage, published_at)` index that depends on
  `btree_gist`, and `ST_Intersects` against a `geography(Geometry, 4326)` column.
  None of it is checkable by the Go compiler.
- Every SQL string in the repositories is unexecuted. The Phase 2 feed query is
  the one to distrust most — three CTEs, two lateral joins, a JSON aggregate and
  a row-comparison keyset predicate, none of which has ever been parsed by
  Postgres.
- No presigned URL has been generated against MinIO, so the S3 wiring is
  compiled but unexercised.
- `go test -race` cannot run here — the toolchain has `CGO_ENABLED=0` and the
  race detector requires cgo. CI runs it on Linux, where it works. This matters
  more in Phase 2 than it did in Phase 1: `moderation.Pipeline` runs layers
  concurrently and writes into a shared slice.
- No `npm install` has run, so the TypeScript dependency versions are unresolved.

Start with `docker compose up -d && make migrate-up`; that is where the
remaining errors are.
