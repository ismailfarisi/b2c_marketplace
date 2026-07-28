# Modules

One package per bounded context. A module owns its tables, its handlers and its
strategy implementations.

```
identity/     users, OTP, sessions, addresses
catalog/      vendors, branches, service types, offerings
scheduling/   time windows, capacity buckets, reservations
ordering/     orders, order lines, the place-order flow
logistics/    fulfillments and their lifecycle
pricing/      quotes, quote lines, tax
payments/     gateway integration, webhooks
ledger/       double-entry accounting
verticals/    one subpackage per vertical — laundry/, meals/, vehicles/
```

## Rules

**Modules do not import each other's internals.** Cross-module access goes
through an interface the *consumer* declares, satisfied by the producer at wiring
time in `cmd/api/main.go`. The compiler is what keeps this monolith modular; once
modules reach into each other's packages, the boundaries exist only in the
directory names.

**Modules do not import `net/http`.** They return typed errors —
`httpx.ErrNotFound`, `*httpx.ValidationError` — and the boundary maps them to
status codes. A module that knows about 404 is a module that cannot be called
from a job.

**Verticals only implement interfaces.** A vertical package implements the four
`servicetype` strategies and registers action handlers. It never adds a case to a
switch in `ordering/`, never adds a column to `fulfillments`, and never appears
by name in `logistics/`.

If one of these gets awkward, that is signal worth taking seriously rather than
routing around — see the health metrics in `docs/DATA-MODEL.md`. A new *service*
vertical should cost 0–2 new tables. Vehicles costs 25, and that is fine: it is a
listing kind with its own domain, not a service riding the Fulfillment engine.

## Shape of a module

```
ordering/
  service.go      business logic; the only file other modules' interfaces describe
  handler.go      implements the generated ServerInterface for its endpoints
  repository.go   sqlc-generated queries wrapped in module-meaningful methods
  errors.go       module-specific sentinel errors
  service_test.go
```

`handler.go` is thin by construction: decode, call service, encode. Any logic
that ends up there cannot be exercised from the worker, and the worker is where
the retries live.
