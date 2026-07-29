# Plan — Admin Console

**Status:** Draft v1 · 2026-07-29
**Supersedes** nothing. This is the first plan for a surface that eleven phases never budgeted — see [PHASE-10 §7](./PHASE-10-HARDENING.md).
**Depends on** nothing that is not already built. Every table this needs exists.

---

## 1. What is actually being asked for

What exists today is a **review console**: nine queues and a decide button. It
was built in a day and it is genuinely useful.

What is being asked for is an **admin console**, and the difference is not more
screens. It is a different question:

> A queue answers **"what needs doing"**.
> An admin console answers **"what is going on with this thing"**.

Those need entirely different backends. A queue is a filtered list with a
mutation. A detail page is an aggregate across eight tables with a timeline, and
nothing in this API returns one.

### 1.1 The number that sizes the work

```
136 tables
105 API paths · 45 of them GET
  0 staff-scoped reads of any single entity
```

Every detail page in this plan needs a **new endpoint**. Not a new screen over an
existing endpoint — a new read model, because the queue endpoints return exactly
the columns a queue row needs and nothing else, which was the right decision and
is now the constraint.

**Roughly 70% of this plan is backend.** Anyone estimating it from the
screenshots will be wrong by a factor of three.

### 1.2 The failure mode this plan exists to avoid

The default admin panel is **CRUD per table**. 136 tables, a list and a form
each, generated. It is fast to build, it demos well, and it is wrong here for
three specific reasons:

1. **It edits around the business rules.** A form that writes `orders.status`
   directly bypasses `ordering.Service`, the capacity release, the ledger
   posting and the outbox event. The constraints in this schema exist because
   the money has to balance; a generic editor is a supported way to unbalance it.
2. **It answers no question anyone asks.** Nobody opens an admin panel to look
   at `quote_lines`. They open it because a customer is on the phone.
3. **Nobody maintains it.** 136 screens nobody chose means 136 screens nobody
   updates, and within two quarters half of them show stale columns.

So this plan is organised around **the questions ops actually asks**, and a table
only gets a surface when a question needs it.

---

## 2. The questions, and what each one costs

Ordered by how soon a launch hits them. This is the actual specification — the
screen inventory in §4 is derived from it.

| # | The question, as someone says it | Needs |
|---|---|---|
| 1 | *"A customer is on the phone about order AE-G3CRYK."* | Order detail: lines, quote, payments, fulfillments, driver, proof, attribution, notifications sent, timeline |
| 2 | *"This vendor says they haven't been paid."* | Vendor detail: compliance state, payout statements, ledger position, branches, staff, open disputes |
| 3 | *"Why is this reel not in the feed?"* | Reel detail: pipeline state, moderation decisions and labels, coverage geometry, author trust, ranking features |
| 4 | *"This driver's cash doesn't reconcile."* | Driver detail: shifts, routes, COD held, remittances, settlements, proof rows |
| 5 | *"Did this customer ever get the OTP?"* | Notification log per user, with suppression reasons |
| 6 | *"Why did this campaign spend nothing?"* | Campaign detail: pacing, reservations, deliveries, charges, targeting |
| 7 | *"Who approved this, and when?"* | Audit trail per entity, from `admin_actions` + `audit_log` |
| 8 | *"Is anything broken right now?"* | Ops health: job loops, queue depths, outbox lag, partition headroom, notification failure rate |
| 9 | *"Find me the seller with this VIN / this phone / this IBAN."* | Global search |
| 10 | *"Change the SMS copy / the CPM floor / the VAT rate."* | Config editors, each with a version history |

**Nine of the ten cannot be answered by any screen that exists.** Number 7 is
partially answerable because `admin_actions` is written correctly; nothing reads
it back.

---

## 3. Design rules

These are the decisions that keep the console from becoming the generic CRUD
panel described in §1.2. Each one costs something.

### 3.1 A detail page is a timeline, facts, and actions — never a form

Three regions, always in this order:

```
┌ facts ─────────────────────────────┐  the state, denormalised, read-only
├ actions ───────────────────────────┤  what a person may do about it
└ timeline ──────────────────────────┘  what has already happened, newest first
```

The timeline is the part that gets skipped and the part that answers the phone
call. `fulfillment_events`, `reel_state_history`, `listing_status_history`,
`subscription_events`, `dispute_events`, `admin_actions`, `ledger_journals` and
`notifications` all already record what happened; nothing merges them.

### 3.2 Every action goes through the service layer

No admin endpoint writes a table another module owns. An admin refund calls
`payments.Refund`; an admin cancel calls `ordering.Cancel`. Where no service
method exists, **the method gets written** — the admin path is not a licence to
skip it.

This is the rule that costs the most and is worth the most. It is also the rule
that will be argued against on the first incident, and the argument will be
"just this once".

**No SQL console. Ever.** A read-only one is a PII exfiltration tool with a nice
UI; a writable one is a way to unbalance the ledger.

### 3.3 Every mutation writes `admin_actions` in the same transaction

The table exists and has the right shape — `actor_id`, `action`, `subject`,
`reason`, `before_state`, `after_state`, `request_id`. Phase 7 wired it for
disputes and nothing else.

`reason` is **required on every destructive action**, for the same reason a
rejection needs one: six months later, a refund with no stated reason is
indistinguishable from an unauthorised one.

### 3.4 Reading PII is itself an event

Opening a customer's address, phone, or a KYC document is an access to personal
data, and PDPL treats it that way. Those reads write `audit_log` with
`actor_type='admin'`.

This is the rule most likely to be cut as gold-plating. It should not be: the
question a regulator asks after a breach is not "was it encrypted", it is
**"who looked at it"**, and a system that cannot answer has no answer.

Practically: a `RecordAccess` middleware on the routes that return PII, not a
blanket audit of every GET — an audit log that records everything is one nobody
reads.

### 3.5 Permissions get real, before there are more than five staff

Today: `platform_admins.role ∈ {moderator, ops, superadmin}` and a `queues`
array nothing reads.

That is already too coarse. A content moderator needs to see a reel and its
author's strike record. They do **not** need a customer's home address, a
vendor's IBAN, or a refund button — and today `RequireStaff` gives them the
first two the moment a detail page exists.

Proposed, and deliberately small:

| Permission | Grants |
|---|---|
| `content.review` | Reel, comment, message, listing queues and detail |
| `content.strike` | Issue and void strikes; decide appeals |
| `commerce.read` | Orders, subscriptions, fulfillments — **without** contact details |
| `commerce.contact` | Recipient name, phone, address on those records |
| `money.read` | Ledger, payouts, invoices, COD |
| `money.write` | Refunds, credits, statement issue, write-offs |
| `identity.review` | KYC cases and documents; bank account verification |
| `config.write` | Templates, rate cards, tax rates, packages, flags |
| `staff.admin` | Grant and revoke the above |

Roles become named bundles of these, stored — not an enum. A new role at launch
should be a row, not a migration.

**`money.write` and `identity.review` should never be held by the same person by
default.** Approving a bank account and then paying it is the whole of payout
fraud in two clicks.

### 3.6 One endpoint per page, not one per panel

An order detail page touches `orders`, `order_lines`, `price_quotes`,
`quote_lines`, `payments`, `fulfillments`, `fulfillment_events`,
`proof_of_delivery`, `cod_collections`, `order_attributions`, `notifications`
and `admin_actions`. Twelve tables.

That is **one** `GET /admin/orders/{id}` returning a composed document, not
twelve requests from the browser. The queue endpoints established this pattern
already (KYC cases return their documents); detail pages need it more.

### 3.7 Impersonation: "view as", never "act as"

Ops will ask for it, because *"the vendor says the button is missing"* is
otherwise unanswerable. It is also the single most dangerous feature in any
admin console.

The line this plan draws: **read-only impersonation, time-boxed, banner-marked,
audited, and no mutations.** A support agent can see the console the vendor
sees. They cannot place an order, change a price, or accept an offer as them. If
something needs doing on the vendor's behalf, it happens through an admin action
with the admin's name on it.

Deferred past the first release. Named here so it is a decision rather than a
patch.

---

## 4. The screens

Grouped as they would appear in the sidebar. **Bold** is new; the rest exists.

### 4.1 Work — the queues that exist

Overview · Reels · Comments · Messages · Appeals · Listings & fraud · Campaigns ·
KYC · Disputes

Additions to what is there:

- **Claim/release wired to `moderation_assignments`.** The table exists, expires
  stale claims lazily, and nothing calls it — so two moderators can still open
  the same reel.
- **Filters and search within a queue.** Every queue is oldest-first capped at
  100. Correct at launch volume, wrong at the first backlog.
- **Signed media.** The reel queue cannot play video (`PHASE-10 §7.3`). This is
  the single largest functional gap in what exists.

### 4.2 Entities — all new

Each is a facts / actions / timeline page per §3.1.

| Page | Route | Composed from | Actions |
|---|---|---|---|
| **Order** | `/admin/orders/{id}` | orders, lines, quote, payments, fulfillments, events, proof, COD, attribution, notifications | refund, partial refund, cancel, reschedule, resend OTP, comp credit, open dispute |
| **Customer** | `/admin/users/{id}` | users, addresses, orders, subscriptions, wallet, notifications, strikes, appeals, blocks | suspend, unsuspend, resend OTP, merge duplicate, export data (PDPL), delete (PDPL) |
| **Vendor** | `/admin/vendors/{id}` | vendors, branches, staff, KYC, payout account, statements, ledger position, disputes, reels | approve onboarding, suspend, set commission override, force payout hold |
| **Branch** | `/admin/branches/{id}` | branch, hours, calendar days, windows, capacity buckets, offerings, drivers | edit hours, add calendar exception, adjust capacity, close for the day |
| **Reel** | `/admin/reels/{id}` | reels, renditions, moderation jobs/labels/decisions, state history, coverage, features, counters, actions | re-run moderation, force publish, take down, re-resolve coverage |
| **Listing** | `/admin/listings/{id}` | listing, photos, specs, price history, status history, offers, leads, inspection, history checks, seller trust | approve, reject, take down, mark sold, refund package |
| **Driver** | `/admin/drivers/{id}` | drivers, documents, shifts, routes, stops, pings, COD held, remittances, proof | activate, suspend, settle cash, force-release routes |
| **Subscription** | `/admin/subscriptions/{id}` | subscription, plan, cycles, selections, pauses, skips, modifications, events, payments | pause, resume, skip, cancel, credit |
| **Campaign** | `/admin/campaigns/{id}` | campaign, targeting, daily spend, deliveries, charges, payments | approve, reject, pause, refund unspent |
| **Dispute** | `/admin/disputes/{id}` | dispute, messages, events, subject, linked refund | assign, resolve, escalate |

### 4.3 Money — all new

| Page | What it answers |
|---|---|
| **Ledger explorer** | Journals by event type, date, account, reference. The one place "where did this money go" is answerable. Read-only, always |
| **Trial balance** | Every account's balance, and whether debits equal credits. A daily green tick, and the alarm when it is not |
| **Payout runs** | Run history, per-vendor statements, who is blocked and why, issue and pay actions |
| **COD reconciliation** | Cash held per driver, declared remittances, settled, shortfalls |
| **Invoices** | Search by number, reprint, credit note, ZATCA chain status |
| **Refunds** | Every refund with its reason and who authorised it. The list nobody wants to look at and everybody eventually does |

### 4.4 Supply & demand — all new

| Page | What it answers |
|---|---|
| **Feed inspector** | Given a point and a viewer, what would the feed return and *why* — score components per candidate. Answers "the feed looked wrong at 14:20" |
| **Coverage map** | Which districts have how many published reels. The content-supply risk (§4 of the video-first plan) made visible |
| **Conversion funnel** | The Phase 3 numbers with their definitions attached |
| **Vendor supply health** | Who has published nothing in 14 days. The list the content operator works from |

### 4.5 Configuration — all new

Every one of these is a table that is edited today with `psql`.

| Page | Table | Guard |
|---|---|---|
| **Notification templates** | `notification_templates` | Version on write; placeholder validation; SMS segment cost shown for Arabic |
| **Service types & offerings** | `service_types`, `service_offerings` | — |
| **Listing packages** | `listing_packages` | Price changes never affect purchased entitlements |
| **Boost rate cards** | `boost_rate_cards` | Date-ranged; never edited in place |
| **Tax rates** | `tax_rates` | Date-ranged; an exclusion constraint already prevents overlap |
| **Tax registrations** | `tax_registrations` | Shape-checked; required before any invoice can issue |
| **Ranking profiles** | `ranking_profiles` | Change is logged and the active profile is named in every feed response |
| **Feature flags** | *(not built)* | Needs the table from DATA-MODEL §14 |
| **Staff** | `platform_admins` | Roles, locales, queues; `staff.admin` only |

### 4.6 Health — all new

| Page | What it answers |
|---|---|
| **Job loops** | Last run and outcome per loop. **A worker that stops is invisible today** — the API stays green and routes go unplanned until a customer calls |
| **Outbox lag** | Oldest unpublished event |
| **Partitions** | Headroom on `notifications`, `boost_deliveries`, `driver_location_pings`, `feed_impressions`. A DEFAULT-partition row blocks the range for good |
| **Notification delivery** | Sent, failed, suppressed by reason — the three need different people |
| **Moderation SLA** | Breach rate over time, not just now |

---

## 5. Phasing

Ordered by what a launch hits first, not by what is easiest.

| Phase | Weeks | Deliverable | Why here |
|---|---:|---|---|
| **A · Support desk** | 3 | Global search; order, customer, vendor detail; timeline component; `admin_actions` on every action; the permission model from §3.5 | Without these, the first support call is answered with `psql`. This is the one phase that is not optional |
| **B · Money** | 3 | Ledger explorer, trial balance, payout runs, COD reconciliation, refunds, invoices | The first payout run is the first time anyone needs to explain a number |
| **C · Supply** | 3 | Reel detail with the pipeline, **signed media so reels can be watched**, listing detail, driver detail, feed inspector | Fixes the largest gap in the console that exists |
| **D · Config** | 2 | Template, rate card, tax, package and staff editors, each with history | Removes the last routine reason to open `psql` in production |
| **E · Health & analytics** | 2 | Job loops, partitions, delivery rates, SLA trend, funnel, supply health | Deferrable only because §3 of the runbooks covers the same ground manually |

**~13 weeks.** For comparison, Phases 8–10 together were 10. This is not a small
project and estimating it as "some screens" is how it ends up half-built.

### 5.1 What Phase A actually contains

Because it is the one that must be right, and because it establishes patterns
every later phase copies:

**Backend (the bulk of it)**

- `internal/modules/adminview` — a read-model package. Composes; owns no tables;
  never writes. Its whole job is the §3.6 one-endpoint-per-page rule, and
  keeping it in one package is what stops twelve modules growing an
  `AdminSummary` method each.
- `GET /admin/search?q=` — reference numbers (`AE-…`, `INV-…`, `DSP-…`,
  `BST-…`, `VL-…`), E.164 phones, VINs, IBAN last-4, uuids. One endpoint, typed
  results.
- `GET /admin/orders/{id}`, `/admin/users/{id}`, `/admin/vendors/{id}`
- `GET /admin/{type}/{id}/timeline` — the merge across the eight history tables
- `POST /admin/orders/{id}/refund|cancel|reschedule|credit`, each through the
  owning service, each writing `admin_actions`
- Permissions: `admin_roles`, `admin_role_permissions`, `admin_grants` (3 tables),
  and a `Require(permission)` middleware replacing `RequireStaff`
- PII access logging on the routes that return contact details

**Frontend**

- Search in the app bar, keyboard-first — a support agent has the customer on
  the phone
- The three detail pages
- A `Timeline` component every later page reuses
- A `Facts` grid and an `Actions` bar with the reason prompt built in

**Not in Phase A:** anything that edits configuration, anything that moves money
beyond a refund, impersonation, bulk actions.

---

## 6. Risks

| Risk | Why it is serious here | Mitigation |
|---|---|---|
| **It becomes CRUD per table** | 136 tables, and generated screens are always available. The result is unusable and unmaintained | §1.2 and §3.2. A table gets a surface when a question in §2 needs it, and the plan names the question |
| **The admin path skips the service layer** | The first incident where a service method is missing, under time pressure. Then it is precedent | §3.2 as a review rule, and the read-model package physically cannot write |
| **Permissions stay `RequireStaff`** | Detail pages turn every moderator into a holder of every customer's address | §3.5 is in Phase A, not deferred. Coarse permissions cost nothing before there is data and are a migration afterwards |
| **PII access is not logged** | Cheapest thing to cut and the one a regulator asks about | §3.4, scoped to routes that return PII so the log stays readable |
| **Effort is estimated from screenshots** | 70% of this is backend and none of it is visible | §1.1 |
| **Impersonation arrives as a patch** | It is asked for during an incident, which is the worst moment to design it | §3.7 draws the line now |
| **The console becomes the product** | Ops tooling absorbs engineering time indefinitely because every request is reasonable | Phases D and E are explicitly deferrable; A–C are not |

---

## 7. What this plan does not include

Stated so the omissions are decisions.

- **No generic CRUD, no SQL console, no bulk editor.** §1.2, §3.2.
- **No write access to `ledger_entries` under any circumstance.** A correction is
  a new balancing journal. There is no admin screen that can do otherwise.
- **No customer-facing content editing.** A vendor's dish description is theirs;
  an admin who can rewrite it is an admin who will be asked to.
- **No analytics warehouse UI.** ClickHouse has its own tools and rebuilding them
  badly is a poor use of the budget.
- **No mobile layout.** Ops runs on a desk. The tablet case is a manager glancing
  at queue depth, which the collapsed sidebar already serves.

---

## 8. Open questions

1. **Who staffs this?** The permission model in §3.5 assumes at least three
   distinct roles — content, commerce, money. With two people it is ceremony;
   with ten it is a launch requirement. This changes Phase A's scope more than
   anything else in the plan.
2. **Is impersonation needed at launch?** §3.7 defers it. If support cannot
   function without it, it moves into Phase A and brings its audit requirements
   with it.
3. **Signed media: one endpoint or two?** Posters and KYC documents have
   different access controls — a poster is a thumbnail, an Emirates ID scan needs
   its own access log. Two endpoints is the safer answer and costs a day.
   Blocking Phase C, and already open in [QA-FINDINGS](./QA-FINDINGS.md) §A.
4. **Does ops need the console in Arabic?** §4 assumes not — the chrome is
   English by decision. A locally-hired moderation team may make that wrong, and
   retrofitting is the same expensive exercise the customer app avoided by doing
   it in Phase 0.
5. **Where does the console live?** Same origin as today, or `ops.` behind an
   allowlist. Detail pages sharpen this considerably: the console stops being a
   queue viewer and becomes a place where every customer's address is one search
   away.

---

## 9. A health metric

Borrowed from DATA-MODEL's, because the same discipline applies.

> **Every screen in this console should be traceable to a question in §2.**

If a screen cannot be, it is there because a table existed, and that is the
generic-CRUD failure arriving one page at a time. The check costs a sentence in
a pull request.

The second, sharper one:

> **No admin action should exist that a service method does not.**

If the console can do something the API cannot, the business rule lives in the
console — and the console is the one place with no tests.

---

## 10. Immediate next step

Two things block a start, and both are §8:

1. **Question 1** — how many people, in how many roles. It sizes Phase A.
2. **Question 3** — one media endpoint or two. It is a day of work and it
   unblocks the largest gap in what already exists.

Everything else in Phase A can begin without an answer.

---

# Built — 2026-07-29

All five phases implemented in one pass. What follows is what exists, and
honestly, what does not.

## Delivered

**A · Support desk.** Migration 20 (11 permissions, 6 roles, expiring grants,
existing staff migrated across). `adminview` read models. Global search over
eight identifier shapes. Order, customer and vendor detail pages with merged
timelines across five history tables. Refund and cancel actions through the
owning services, each writing `admin_actions` with a required reason. PII reads
audited.

**B · Money.** Ledger explorer, trial balance, payout runs and statements with
per-vendor block reasons in words, COD reconciliation, remittances, refunds,
invoices. Four screens.

**C · Supply.** Signed staff playback for unpublished media — the gap this plan
called the largest. Reel detail with the pipeline as four timestamps. Coverage
and supply-gap reporting.

**D · Config.** Feature flags and app settings, each change writing its audit row
in the same transaction as the change.

**E · Health.** Background-loop staleness, outbox lag, partition headroom,
notification delivery split three ways, moderation SLA by day.

## Deliberately not built, with reasons

*(Superseded — everything listed here has since been built. Kept because the
reasons were the design, and the implementations follow them.)*

## Found while building

Six defects, in `QA-FINDINGS.md` §11–16. Three are worth repeating here because
they are the argument for the console rather than incidental to it:

- **Card orders never captured.** `payments.Capture` was correct and called by
  nothing. Card orders never reached the ledger at all; vendor payables were
  computed from cash and subscriptions only, and looked entirely plausible.
- **No PII read had ever been audited.** An `inet`/`text` mismatch, failing into
  a warn log that nothing read. The console reported it was recording who read
  what, and recorded nothing.
- **Platform ledger accounts duplicated per posting.** A `UNIQUE` constraint over
  a nullable column, so `ON CONFLICT` never fired for platform-level accounts.
  Found on the trial balance's first render.

Each of these was invisible from inside the running system and became obvious the
moment something read the data back. That is the case for the console, made
better than the plan made it.


---

# Completed — 2026-07-29, second pass

The five items the first pass deferred are built. Each was deferred with a
reason, and in every case the reason turned out to be the specification.

## Payout actions

Deferred because `payouts.Service` had no method taking an actor that would
refuse a blocked vendor. It has three now. `Pay` re-checks eligibility at the
moment money moves rather than trusting the gate that ran at issue — a statement
is issued days before it is paid and a licence can lapse in between, with nothing
writing to the vendor's record when the date passes. Verified: a licence expiring
between the two steps refuses the payment, and the same statement pays cleanly
once renewed.

Approval is separate from issuing and issues nothing. A run with zero statements
cannot be approved, because that is the shape a failed generation takes. Bulk
issue skips blocked vendors and returns the list rather than failing the batch.

## Versioned editors

Deferred because editing a tax rate in place restates every invoice issued under
it. They supersede: the current row is closed and a new one opened in one
transaction, and effective dates must be in the future.

Templates version rather than overwrite, and a revision that drops a placeholder
the old body used is refused — nothing downstream renders a template twice to
check, so a delivery-code template that loses `{{code}}` sends a blank message
and the first sign is a driver at a door.

The segment counter is the reason the template editor exists. 75 English
characters is one SMS; 80 Arabic characters is two, because anything outside
GSM-7 drops the budget from 160 to 70. One en-dash pasted from a document does
the same to an English message.

## Listing, driver, feed inspector

The inspector lives in the `feed` package and calls the same `retrieve` the feed
calls. A copy in a console package would diverge on the first change to either,
and an inspector inspecting a different feed is worse than none — it is
confidently wrong.

## Account lifecycle

Deferred because no safe method existed. The design that emerged is that a merge
**links and freezes rather than moves**: 58 tables reference `users`, and a tax
invoice whose buyer changed after issue is a different document from the one the
customer holds. Addresses, favourites and saved searches move; orders and
invoices stay, and the count of what stayed is returned so the operator knows
before the customer asks.

Erasure is pseudonymisation for the same reason. The platform must retain
invoices; a system that pretended to delete them would be lying to the customer
and breaking the law in one operation.

## Impersonation

The read-only guarantee is one middleware on the whole authenticated router,
refusing every unsafe method before routing — not a per-handler check, which
protects exactly the handlers somebody remembered. Fifteen minutes, not
refreshable, no session row, closed to the console, and refused against a
colleague.

---

## What is genuinely left

- **`RequireStaff` on the older queues.** The moderation, KYC and dispute queues
  still use the Phase 2 boolean rather than the §3.5 permissions. Each migrates
  when it gains a detail page; the console half is done and the queues are not.
- **A job heartbeat table.** Loop health is inferred from the artefacts each loop
  produces, and the health page says so. A loop with nothing to do is
  indistinguishable from one that has stopped until the loops write heartbeats.
- **Weight tuning from the feed inspector.** It explains the ranking; it does not
  let anyone change it. Editing `ranking_profiles` needs the same versioned
  treatment the tax rates got, plus a way to see the effect before committing.

## Defects found across both passes

Twenty, in `QA-FINDINGS.md`. Six are worth repeating because they are the
argument for the console rather than incidental to it — every one was invisible
from inside the running system and became obvious the moment something read the
data back:

- Card orders authorised and never captured, so no card sale ever reached the
  ledger.
- No PII read had ever been audited: an `inet`/`text` mismatch failing into a
  warn log nothing read.
- Platform ledger accounts duplicating once per posting, because `ON CONFLICT`
  cannot fire on a nullable column under a plain `UNIQUE`.
- Cancelling an order never returned the delivery slot.
- A context key declared with no value, silently aliasing the acting user to the
  staff member during impersonation.
- A nil slice turning a `NOT (id = ANY(...))` filter into `NULL` and emptying the
  feed with no error.
