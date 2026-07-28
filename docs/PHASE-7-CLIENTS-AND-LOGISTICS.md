# Phase 7 — Clients & Logistics

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 7 and [PLAN.md](./PLAN.md) §5 P3–P4
**Scope:** drivers, shifts, route building, assignment, live tracking, proof of delivery, cash on delivery, the vendor console, payout statements, disputes, the admin audit trail, and a web driver view.
**Out of scope:** the native driver app (§3), notifications (Phase 8), KYC (Phase 8), the customer mobile app (§7 — going to KMM).

PLAN.md §1 made a claim about the Fulfillment primitive and repeated it above
`CREATE TABLE fulfillments`:

> Dispatch, routing and the driver app read this table and nothing else. None of
> them ever learns what a laundry bag or a meal box is — that is the entire
> point, and it is what lets a fifth vertical ship without touching logistics.

Six phases later, this is the phase that either proves that or does not.

---

## 1. Two seams, two exams

| Phase | Seam | Question | Verdict |
|---|---|---|---|
| 5 | `service_types` (downstream) | Can a vertical be added by registering strategies? | Held; found a missing axis (recurrence) |
| 6 | `action` (upstream) | Can the feed gain a supply engine without learning what a car is? | Held; forced one honest fix (`payments`) |
| **7** | **`fulfillment` (downstream)** | **Can logistics be built without knowing what is in the bag?** | **Held. See §2.** |

---

## 2. The gate: does the Fulfillment primitive hold?

**Yes, and it is enforced by a test rather than by this paragraph.**

`internal/app/seam_test.go` — written in Phase 6 for the action seam — now also
parses the imports of `modules/dispatch` and `modules/driver` and fails the build
if either reaches for `ordering`, `catalog`, `console`, any vertical, or
`subscriptions`.

What logistics actually reads:

```sql
SELECT f.id, COALESCE(f.order_id, f.subscription_id), f.sequence, f.kind,
       ST_Y(f.point), ST_X(f.point), w.starts_at, w.ends_at
  FROM fulfillments f JOIN time_windows w ON w.id = f.window_id
 WHERE f.branch_id = $1 AND f.scheduled_date = $2
   AND f.status = 'scheduled' AND f.route_id IS NULL
```

Three tables: the primitive, its window, and the branch it departs from. No
`order_lines`, no `menu_items`, no `subscription_meal_selections`, no
`vehicle_listings`.

### 2.1 The one place it looked like the seam would break

Precedence. A laundry order must be collected before it is returned, and that
looked like knowledge only the laundry vertical could have.

It is not. The seam already carries `(parent, sequence)`:

```go
func precedes(a, b Stop) bool {
    return a.ParentID == b.ParentID && a.ParentID != uuid.Nil && a.Sequence < b.Sequence
}
```

Dispatch learns that sequence 1 precedes sequence 2 within a parent. It never
learns why, and `TestPickupPrecedesDeliveryWithoutKnowingWhatLaundryIs` rigs the
geometry against it — the delivery is next door to the kitchen and the pickup is
across the city — so pure nearest-neighbour would get it wrong every time.

`COALESCE(order_id, subscription_id)` is the parent. That single expression is
the entire extent of what dispatch knows about verticals.

### 2.2 Where the seam is deliberately *not* applied

`internal/modules/console` reads `order_lines` and `menu_items` directly, and
that is correct rather than a leak. The distinction:

- **Dispatch** answers *"where does the van go"* — the same question for a meal
  box and a laundry bag. Seam only.
- **The console** answers *"what does my kitchen cook by six"* — meaningless
  without knowing what a meal is.

Forcing the console through the seam would produce a dashboard that shows a
vendor a list of addresses, which is the screen nobody can run a business from.
**The seam is a rule about questions, not about layers**, and `console` is
excluded from the import test on exactly that ground.

The same split shows up in the API: `ManifestStop` (driver) carries no item
names; `QueueItem` (console) does.

---

## 3. The driver app decision, and what taking the advice actually costs

PLAN.md §5 P4 is unusually direct:

> *The driver app with background location is the single most expensive item in
> this plan. Consider launching Phase 3 with vendors doing their own delivery
> and a simple web-based driver view, and defer the native driver app until
> order volume justifies it.*

Taken. There is no native app; `apps/web/[locale]/driver` is the client, and the
API is client-agnostic so building the native one later costs a client and not a
backend.

**What that costs, stated plainly:** a browser tab does not report location in
the background. `navigator.geolocation.watchPosition` stops when the screen locks
or the tab is backgrounded. So:

- ✅ Arrival geofencing, proof-of-delivery position, distance-from-address on the
  proof row — all work, because the driver is looking at the screen at the door.
- ❌ A customer-facing "your driver is 4 minutes away" map does not work.

That map is the thing that would justify the native app, and it is a product
decision rather than an engineering one. Location reporting also runs *only while
a stop is live* — a driver with the tab open at home is not on a delivery, and
pinging them anyway is surveillance the product cannot justify.

---

## 4. Routing

Nearest-neighbour construction, then precedence-preserving 2-opt. Not optimal,
and optimality is not the goal: at twenty stops the gap between this and an exact
solver is a few percent, while the gap between *any* sequencing and the arrival
order is often 40%. The first gap is the one worth closing.

Decisions worth defending:

**Grouped by time window before sequencing.** A route mixing the 09:00–12:00 and
18:00–21:00 windows is a driver sitting in a car park for five hours, and no
amount of clever sequencing fixes it — the constraint is the promise made to the
customer, not the geometry.

**Split after sequencing, never before.** Splitting first and sequencing each
half independently produces two routes that cross the city through each other,
which is the classic way to make a router look worse than no router.

**Deterministic.** Ties break on fulfillment id. A driver whose manifest
reshuffles every time the planner runs stops trusting it, and then stops
following it.

**No routing API.** A 1.35 detour factor over great-circle distance is a lie
everywhere, but it is a *consistent* lie, which is what an ETA needs. The
alternative is 400 API calls to plan twenty routes, on every replan.

**Waiting is modelled.** A driver arriving before a window opens waits, and the
wait pushes every subsequent ETA. Omitting it makes early-morning routes look an
hour faster than they are — exactly where the promised time matters most.

**Lateness is computed at plan time**, returned by the planning endpoint and
shown on the dashboard. A route that cannot be delivered on time is a
reassignment decision now, not a customer discovery in three hours.

---

## 5. Cash on delivery — beyond the plan's list, deliberately

Not in PLAN.md §5 P4. Included anyway, for two reasons.

`driver_float` has been in `ledger_account_type` since migration 000001 with
nothing posting to it — the same situation `deferred_revenue` was in before Phase
5, and it was put there for exactly this. And more importantly: **cash is the
dominant payment method for delivery in this region.** A delivery product that
cannot take cash is not a delivery product here, and discovering that after the
driver app ships means rebuilding the completion flow with money in it.

The account has one meaning and everything follows from keeping it true:

> The balance of a driver's float account is exactly the cash they are holding
> that the platform has not received.

```
collect    Dr driver float      Cr vendor payable / platform revenue / VAT payable
remit      Dr platform cash     Cr driver float
write off  Dr platform revenue  Cr driver float
```

**A shortfall has no posting of its own.** A driver who declares 900 and hands
over 850 gets a remittance of 850; the 50 stays in their float, because they are
still holding it and the sentence above is still true. Only a decision to stop
pursuing it moves money.

Two more:

- **Commission is taken on what was *collected*, not on what was owed.** A
  customer who pays 240 on a 250 order has paid 240; charging the vendor
  commission on 250 is a bill they will query and win.
- **The driver declares, the branch counts.** Separate fields, separate moments,
  separate people. Collapsing them means the system can never represent a
  shortfall — and a system that cannot represent one gets one anyway, silently.

The write-off is debited to `platform_revenue`, which is contra-revenue rather
than the bad-debt *expense* an accountant would want. The chart of accounts has
no expense accounts at all, so contra-revenue is the internally consistent choice
rather than the correct one. Flagged in §8, not quietly assumed.

---

## 6. Proof of delivery

Graded by what is at stake, and the grading is sent with the manifest so the
driver knows before they are at the door:

| Situation | Required | Why |
|---|---|---|
| Any cash | **OTP** | "The driver says they collected AED 240" is not a defensible position |
| Pickup | Photo | The customer is present by definition; the disputed question is *what was collected* |
| Delivery | Recipient name | The disputed question is whether anyone received it |

`left_at_door` is never sufficient and is named honestly so nobody builds a
report that treats it as evidence — it proves the driver says so and nothing else.

The OTP is four digits, not six. This is a code read aloud at a door, not an
authentication factor: the attacker who could brute-force it is standing in front
of the customer, and six digits costs every customer reading two more numbers to
a driver in the rain. Only the hash is stored — a database reader who can see
every OTP can mark every delivery complete.

Every completion records **how far the driver was from the address**. Not a
refusal — drivers legitimately complete from a lobby or a service road — but it
turns "it never arrived" from an opinion into "the driver was 40 m away" or "the
driver was 3 km away".

Three smaller rules with reasons:

- **A driver cannot cancel work**, only fail it with a reason from a closed list.
  A driver who could cancel has a way to make their route shorter and the
  incentive points exactly the wrong way.
- **`arrived` can go back to `en_route`.** Without it, a driver who marked the
  wrong building fails the delivery and redelivers, and the failure statistics
  stop meaning anything.
- **Ending a shift releases undelivered stops.** Leaving them assigned means the
  planner skips that work forever — the fulfillments carry a `route_id` and look
  handled. This is the failure mode that makes a dispatch system quietly lose
  orders, and it is invisible until a customer calls. A worker sweep catches the
  same shape for routes assigned but never started.

---

## 7. Clients

**Web — built.** Branch picker, branch dashboard (live metrics, dispatch board,
order queue, one-click replan), and the driver view. Both locales, logical CSS
properties throughout, `tsc --noEmit` clean and `next build` green.

**Mobile — deliberately not built.** The reel feed is going to Kotlin
Multiplatform for video performance. That is a bigger decision than it looks and
it changes one thing structurally:

> **`api/openapi.yaml` is now the cross-language contract, not a description of
> one.** `packages/api-client` serves web only and can never be the mobile
> client, so the spec is the single artefact both languages generate from. It
> should be treated accordingly — `make gen` needs a Kotlin target alongside the
> TypeScript one, and spec drift stops being a documentation problem and becomes
> a compile error in two languages.

The Phase 7 spec additions were written with that in mind: every response is a
named schema rather than an inline object, so a Kotlin generator has something to
name.

**Found while wiring the console:** `packages/api-client/src/http-client.ts` has
never typechecked. `exactOptionalPropertyTypes` is on and the `fetch` call passed
`body: undefined`, which is a different type from an absent `body` under that
flag. It shipped in Phase 0 and nothing ran `tsc` over it until now, because it
had no consumer. Fixed.

---

## 8. Known limitations

Surfaced rather than fixed, roughly in the order each is likely to bite.

1. **The delivery OTP is issued and never sent.** `Dispatch` generates it and
   returns it to the handler; notifications are Phase 8. Until then a customer
   cannot receive a code they will be asked for — so in practice cash deliveries
   are blocked, since cash requires OTP proof. This is the sharpest gap in the
   phase and it is one endpoint away from closing.
2. **Bad-debt write-offs are contra-revenue** (§5). Correct would be a
   `bad_debt_expense` account. That is a chart-of-accounts decision tied to open
   decision #4, and adding an enum value on the ledger's behalf is not this
   phase's call.
3. **The web token store keeps the refresh token in `sessionStorage`.** Not
   localStorage — tab-scoped, dies with the tab, which matches how a shared
   branch machine is used. The correct answer is an httpOnly cookie set by a
   same-origin BFF route, which needs a server route per call. Phase 8.
4. **`vendor_id` is not checked on route assignment.** A dispatcher who can reach
   `POST /routes/{id}/assign` can assign any route. Every other console route is
   branch-authorised; this one is not, and it needs a `console.AuthorizeBranch`
   against the route's branch.
5. **Statement regeneration silently skips issued statements.** Correct — a
   document a vendor reconciled against must not be restated — but a late refund
   after issue is currently invisible. It should become an adjustment line on the
   *next* period, and nothing writes one.
6. **`driver_documents` has no expiry enforcement.** The table, the state machine
   and the expiry index exist; no job reads them. A driver whose insurance lapsed
   is a liability the platform is carrying without knowing it.
7. **Routing has no live traffic and no time-of-day speed model.** 24 km/h flat.
   Deliberately pessimistic, so ETAs skew early — which costs nothing — but a
   Ramadan iftar rush and a Tuesday morning are not the same city.
8. **`payout_runs.state` is never advanced past `draft`.** Approving and paying a
   whole run is an ops screen that does not exist; statements are paid
   individually. `PostVendorPaid` works, `Pay()` works, the batch does not.
9. **The subscription roster query joins `subscription_meal_selections`, which is
   partitioned by `service_date`.** A roster request for a date outside the
   materialised partition range hits the DEFAULT partition. Correct results,
   quietly worse plan.

---

## 9. What shipped

**Migration `000017_logistics_console`** — 20 tables:

| Group | Tables |
|---|---|
| Drivers | `drivers`, `driver_documents`, `driver_shifts`, `driver_location_pings` (partitioned) |
| Routing | `delivery_routes`, `route_stops`, `fulfillment_assignments` |
| Completion | `proof_of_delivery`, `delivery_attempts` |
| Cash | `cod_collections`, `cod_remittances` |
| Payouts | `payout_runs`, `payout_statements`, `payout_statement_lines` |
| Disputes & audit | `disputes`, `dispute_events`, `admin_actions` |

Plus five columns on `fulfillments` (`driver_id`, `route_id`, `attempt_count`,
`otp_hash`, `otp_issued_at`) — none of which describes *what* is being delivered
— a shared `generate_reference(prefix)`, and rolling ping partitions.

**Go**

| Package | What it is |
|---|---|
| `modules/dispatch` | Haversine, precedence, nearest-neighbour + 2-opt, ETA planning with waits, splitting, route persistence, the board |
| `modules/driver` | Driver state machine, graded proof, OTP, geofence, manifest, completion with cash, remittances |
| `modules/console` | Vendor auth, order queue, capacity calendar, subscription roster, live summary |
| `modules/payouts` | Statement arithmetic and periods (pure), ledger-derived generation, issue, pay |
| `modules/admin` | Disputes with per-kind SLAs, the transactional audit trail |
| `modules/ledger/logistics.go` | Cash collection, remittance, write-off, vendor payout |
| `api/logistics.go`, `api/console.go` | 22 endpoints |
| `cmd/worker/logistics.go` | Route planning, stale-route release, partition maintenance, ping retention, weekly payouts |

**Web** — 4 routes across 2 locales, `apps/web/src/lib/api.ts` bindings, token
store, console + driver styles.

**Tests** — 22 packages passing. New: 16 in `dispatch`, 12 in `driver`, 15 in
`payouts`.

**Verified:** `gofmt`, `go vet`, `go build`, `go test -count=1 ./...` clean;
`tsc --noEmit` clean across all three TS workspaces; `next build` green;
`redocly lint` valid with 32 cosmetic warnings.

**Not verified.** The migrations have never been applied — **113 tables across 17
migrations, none of which has ever been parsed by Postgres.** This one adds a
partitioned table with a DO-block reaper, a partial unique index that enforces
single-dispatch, a `CHECK` that a statement balances, and route-planning SQL that
writes four tables in one transaction. The routing arithmetic is tested; the SQL
around it is not.
