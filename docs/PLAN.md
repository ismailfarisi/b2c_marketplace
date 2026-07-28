# B2C Services Marketplace — Technical Plan

> **Superseded on framing by [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md).**
> The engine design in this document — the Fulfillment primitive (§1), the price state machine and ledger (§2), capacity buckets (§2.3), the data model (§4) and the GCC compliance requirements (§7) — all remain correct and in force. What changed is centrality: this engine is now one fulfillment backend behind a shared discovery layer.
>
> **§5's phases, timeline and team size are superseded** by PLAN-VIDEO-FIRST §8 (~40 weeks, ~5–6 engineers). Read §5 as **engine dependency ordering**, not as a calendar. `DATA-MODEL.md`'s per-table `P` column uses §5's numbering — the mapping between the two schemes is in [README.md](./README.md).

**Status:** Draft v1 · 2026-07-28
**Market:** GCC (UAE first, KSA second)
**Model:** Multi-vendor, recurring-first
**Launch verticals:** Laundry, Meal subscriptions — on a generic service engine

---

## 1. The central design decision

Laundry and meal subscriptions look like different products. They are not. Both reduce to:

> A **vendor** sells a **service offering**, consumed through dated, slotted, address-bound **fulfillments**, constrained by **capacity**, paid through a **ledger**.

Everything hinges on making **Fulfillment** the atomic primitive:

| Vertical | Fulfillments |
|---|---|
| Laundry, one-off | 2 — pickup, then return |
| Laundry, weekly plan | 2 per week, generated on a rolling window |
| Meal plan, 5 days/week lunch | 1 per weekday |
| Cleaning (future) | 1 per visit, longer duration |
| Salon at home (future) | 1 per appointment |

A fulfillment is `(vendor, customer, address, date, time_window, type, state, payload_ref)`. **The driver app, routing, dispatch and live tracking operate on fulfillments only — they never know what laundry or a meal is.** That single decision is what makes "etc." cost configuration instead of a rewrite.

### Adding a new vertical = defining four strategies

1. **Item selection** — how the customer picks what they want (itemized list / weight estimate / plan template / menu-per-day)
2. **Fulfillment pattern** — single, pickup+return, or recurring-generated
3. **Capacity resource** — what runs out (kg/day, pickup slots, portions of dish X, technician-hours)
4. **Pricing strategy** — fixed, per-unit, prepaid-plan, or estimate-then-adjust

No new tables. No new endpoints. This is the acceptance test for the architecture.

---

## 2. The three things that will actually hurt

These are the places where naive designs fail. Design for them from day one; retrofitting each is a multi-week rewrite.

### 2.1 Price is a lifecycle, not a column

Laundry cannot know the final price at booking. The customer says "about 4kg", the facility weighs 5.2kg. Meal plans are prepaid in full up front, then partially refunded when the customer pauses for a week.

So: a `price_quote` with line items and a **state machine** — `estimated → authorized → adjusted → captured → (partially) refunded`. Card is **authorized** at booking, **captured** at the adjusted amount after intake. Never store a single `total` and mutate it; you will lose the audit trail the first time a customer disputes a charge.

### 2.2 You need a double-entry ledger. Not "later".

You have: platform commissions, vendor payables, prepaid subscription balances (deferred revenue), pause credits, refunds, driver cash-on-delivery collections, and VAT held on behalf of the government. Payment-gateway records cannot answer "what do we owe this vendor today", and a `vendors.balance` column will drift and then silently be wrong.

Build `ledger_accounts` + immutable `ledger_entries` (always balanced pairs) in Phase 1, while it's five account types instead of thirty. Gateway splits (Stripe Connect etc.) still don't remove this need — they only move money, they don't tell you the story.

### 2.3 Capacity is the concurrency hotspot

Two customers booking the last Tuesday-morning pickup slot at the same instant, or the last 12 portions of Thursday's biryani, is a race. Model it explicitly:

```
capacity_bucket(branch_id, resource_key, date, window_id, limit, consumed)
UNIQUE(branch_id, resource_key, date, window_id)
CHECK (consumed <= limit)
```

Note the key is **branch**, not vendor — a laundry chain has multiple facilities with independent capacity and catchment areas.

Reserve inside the order transaction with `SELECT … FOR UPDATE` (or let the CHECK constraint reject and retry). Hold reservations with a TTL so abandoned carts release. Load-test this specifically — it is the one endpoint that fails under launch-day traffic, and Ramadan meal-plan signups arrive in a spike, not a curve.

---

## 3. Stack

| Layer | Choice | Note |
|---|---|---|
| API | **Go**, modular monolith | `chi` router, `sqlc` for type-safe SQL (prefer over an ORM in Go), `golang-migrate` |
| DB | **PostgreSQL 16** | PostGIS for service areas & routing |
| Jobs | **River** (Postgres-backed) | Subscription cycles, billing retries, notifications. No Redis needed initially |
| Contract | **OpenAPI 3.1** | Source of truth. `oapi-codegen` → Go stubs, `orval` → TS client |
| Web | **Next.js** (App Router) | Customer + vendor + admin as three route groups, one app, shared UI lib |
| Mobile | **Expo (React Native)** | Customer app + driver app, both consume the generated TS client |
| Infra | AWS **me-central-1** (UAE) | Data residency — see §7 |

**The OpenAPI spec is non-negotiable.** It is the only thing that gives a Go backend and three TypeScript clients a shared, compiler-enforced contract. Spec changes must fail CI if generated code is stale.

### Repository layout

```
b2c_marketplace/
├── api/openapi.yaml              # single source of truth
├── services/core/
│   ├── cmd/{api,worker}/
│   └── internal/
│       ├── identity/             # auth, OTP, roles, addresses
│       ├── catalog/              # offerings, variants, plans, menus
│       ├── scheduling/           # windows, capacity, service areas
│       ├── commerce/             # orders, subscriptions, fulfillments
│       ├── money/                # pricing, payments, ledger, invoices
│       ├── logistics/            # routes, drivers, tracking
│       └── platform/             # db, config, telemetry, outbox
├── apps/{web,mobile-customer,mobile-driver}/
├── packages/api-client/          # generated — never hand-edited
└── db/migrations/
```

Modular monolith, not microservices. One deploy, one transaction boundary across order+capacity+ledger — which is exactly where you need atomicity. Split later only if a module proves it needs independent scaling.

---

## 4. Core data model

> Abridged spine below — the full 114-table catalogue with phase tags is in [DATA-MODEL.md](./DATA-MODEL.md).

```
identity     users · addresses · vendors · vendor_branches · vendor_members · drivers
catalog      service_categories · service_offerings · offering_variants
             subscription_plans · menu_cycles · menu_items
scheduling   service_areas(geo) · time_windows · capacity_buckets · blackouts
commerce     carts · orders · order_lines
             subscriptions · subscription_cycles · pauses
             fulfillments · fulfillment_items
money        price_quotes · quote_lines · payment_methods · payments
             ledger_accounts · ledger_entries · payouts · invoices · credits
logistics    routes · route_stops · assignments · location_pings
platform     outbox_events · audit_log · idempotency_keys
```

Cross-cutting rules:
- **Every** mutating endpoint takes an `Idempotency-Key`. Mobile networks retry; double-charging a customer is unrecoverable trust damage.
- **Outbox pattern** for all side effects (push, SMS, WhatsApp, webhooks). Write the event in the same transaction as the state change; the worker delivers it.
- Money as **integer minor units** (fils/halalas) + currency code. Never floats.
- Soft-delete nothing that has money attached.

### The subscription scheduler

A subscription does not pre-generate a year of fulfillments. A nightly job **materializes a rolling 14-day window** of fulfillments from the plan's rules. Pauses and skips only touch unmaterialized or future-materialized fulfillments — never past ones. This keeps pause/resume/plan-change tractable and the table small.

---

## 5. Build order — thin slice first

**Assumption:** ~4 engineers (1 Go, 1 web, 1 mobile, 1 full-stack) + designer. Adjust proportionally.

### Phase 0 — Foundations · ~2 weeks
Monorepo, Postgres schema core, OpenAPI skeleton + codegen wired into CI, phone-OTP auth (the GCC norm — do not lead with email/password), environments, CI/CD.

### Phase 1 — Walking skeleton · ~3 weeks ← *the critical phase*
**One** seeded laundry vendor · **one** offering (wash & fold, per-kg) · customer on **web only** · picks tomorrow's pickup slot · pays with a real test card · order appears in the vendor dashboard · vendor advances it to delivered · ledger records commission + vendor payable.

No apps. No meals. No driver app. No subscriptions.

This deliberately touches every layer — auth, catalog, capacity reservation, order state machine, payment authorize/capture, ledger, vendor console. **If the architecture is wrong, it is wrong here, cheaply.** Do not start Phase 2 until an order flows end-to-end with real money movement.

### Phase 2 — Prove the abstraction · ~3 weeks
Add meals: subscription plans, menu cycles, the rolling-window scheduler, pause/skip, prepaid billing with deferred revenue in the ledger.

**Gate:** if adding meals required changing `commerce` or `scheduling` internals rather than adding strategies, stop and fix the abstraction. This phase is the architecture's exam.

### Phase 3 — Clients · ~5 weeks
Customer mobile app (Expo), responsive web polish, full vendor dashboard (catalog, capacity calendar, subscription roster, order queue, payout statements).

### Phase 4 — Logistics · ~4 weeks
Driver app, route building and assignment, live tracking, proof of delivery, admin console (vendor approvals, disputes, commission config, refunds).

*The driver app with background location is the single most expensive item in this plan. Consider launching Phase 3 with vendors doing their own delivery and a simple web-based driver view, and defer the native driver app until order volume justifies it.*

### Phase 5 — Market readiness · ~4 weeks
Arabic + RTL, VAT, invoicing, vendor KYC, automated payouts, notifications, analytics. See §7 — several of these are legal blockers, not polish.

### Phase 6 — Hardening · ~2 weeks
Load-test capacity contention specifically, observability, on-call runbooks, launch.

**~23 weeks to full scope. First revenue possible at end of Phase 3 (~13 weeks) with manual ops behind the scenes** — strongly recommended.

---

## 6. Payments

Recurring + prepaid + multi-vendor + refunds is the hardest money shape there is.

- **Gateway:** Stripe (supports UAE, best Connect tooling) with Checkout.com or Telr as the local alternative. Validate KSA acquiring separately — it is not the same as UAE.
- **Cards:** tokenize and store off-session for recurring charges. Meal plans in GCC are typically **prepaid per cycle**, not metered — bill the full cycle up front, recognize revenue daily in the ledger.
- **Mandates:** confirm 3DS/SCA requirements for off-session GCC recurring charges with your acquirer before designing the retry flow.
- **Dunning:** failed renewal → retry schedule → grace period → suspend fulfillments (do not delete the subscription). Meal customers must know *before* the delivery doesn't arrive.
- **Payouts:** platform-of-record. You collect, the ledger computes vendor payables, payouts run on a schedule with a downloadable statement.
- **COD:** still meaningful in the region. If supported, drivers hold platform cash — model it as a driver float account in the ledger and reconcile daily.

---

## 7. GCC specifics — treat as blockers, not polish

| Item | Requirement |
|---|---|
| **VAT** | 5% UAE, 15% KSA. Consumer prices must be displayed **tax-inclusive**. Bake into the pricing engine, don't add at checkout |
| **ZATCA (KSA)** | Phase 2 e-invoicing requires cryptographically stamped invoices via Fatoora. **Weeks of work — scope before committing to a KSA date** |
| **Arabic / RTL** | Not a translation pass. Affects layout, date/number formatting, fonts, and the design system. Cheapest if adopted in Phase 0, brutal in Phase 5 |
| **Ramadan** | Meal subscriptions invert — iftar/suhoor windows, changed menus, altered capacity. A first-class scheduling concept, not an edge case |
| **Weekend** | Fri–Sat in some contexts, Sat–Sun in others. Never hardcode; make it vendor/region config |
| **Vendor KYC** | Trade licence, Emirates ID / national ID, IBAN verification before first payout |
| **Data residency** | UAE PDPL / KSA PDPL. Host in-region (`me-central-1`); confirm before choosing any SaaS subprocessor |
| **Addresses** | GCC addressing is landmark-based. Pin-on-map + building/flat/landmark free-text. Postal-code-driven forms fail here |

---

## 8. Top risks

| Risk | Mitigation |
|---|---|
| Abstraction leaks; meals need engine surgery | The meals gate (§5 P2 · video-first Phase 5) exists precisely to catch this while it's cheap |
| Capacity race under launch spike | Explicit bucket model + DB constraints + targeted load test at hardening (§5 P6 · video-first Phase 10) |
| Ledger deferred → money reporting drifts | Build it in Phase 1 when it's small |
| Driver app blows the timeline | Launch with vendor-managed delivery; defer native driver app |
| Cold-start: no vendors → no customers | Launch one city, one district, hand-recruit ~10 vendors. Product cannot fix marketplace liquidity |
| ZATCA discovered late | Scope it *now* if KSA is in year one |

---

## 9. Open questions

1. **Commission model** — flat %, per-vertical %, or subscription fee for vendors? Affects the ledger's account structure.
2. **Who delivers?** Platform drivers, vendor's own drivers, or 3PL? Reshapes the logistics phase (§5 P4 · video-first Phase 7) substantially.
3. **Ramadan timing** — is the first launch before or after? Meal-plan demand and menu logic shift entirely.
4. **KSA in year one?** If yes, ZATCA must be scoped immediately.
5. **Existing ops?** If you already run a laundry or kitchen, seeding the first vendor is solved and Phase 1 gets a real pilot customer.
```
