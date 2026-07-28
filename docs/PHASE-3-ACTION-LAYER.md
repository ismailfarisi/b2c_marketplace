# Phase 3 — Action Layer & Conversion Attribution

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §2 and §8 Phase 3
**Scope:** `reel_actions`, the reel → book loop, and honest conversion measurement.
**Out of scope:** ranking (Phase 4), comments and interest profiles (Phase 4), UGC (Phase 6).

The plan's instruction for this phase is one sentence: *"Instrument conversion
honestly before building ranking."* Everything below follows from taking the
word **honestly** literally.

---

## 1. Why this phase is measurement-shaped

Phase 2 built a feed. Phase 4 will build a ranking model on top of it. Between
them sits the only question that matters: **does a reel actually produce a
booking?**

Getting that number wrong is not a reporting inconvenience. Three things depend
on it, in ascending order of cost:

1. **The egress bill.** PLAN-VIDEO-FIRST §9: every view is paid for whether it
   converts or not, at ~$0.11–0.17/GB in-region. A conversion number that is
   too high justifies scaling reach into a loss.
2. **The Phase 4 ranking model.** It trains on these labels. A flattered number
   does not merely mislead a dashboard — it teaches the model to optimise for a
   fiction, and the error compounds every retrain.
3. **The company's belief about its own product.** The video-first bet is
   Framing B: the feed *is* the product. If the feed does not sell, that is
   worth discovering in week 13 rather than week 40.

So the design goal here is not "attribute as much as possible." It is
**attribute defensibly, and make the shape of the doubt visible.**

---

## 2. Claim versus evidence

The order body has carried `source_reel_id` since migration 000006. That field
is a **claim** made by the client. It is useful and it is not evidence:

- a buggy client attributes orders to whatever reel was last in memory
- a stale deep link attributes an order to a reel the user saw last week
- a generous SDK attributes on any prior view

Any of those inflates the one number the whole bet rests on, and none of them
announces itself.

So Phase 3 adds `action_clicks`: a server-side log of every CTA press. The
client may claim; the server independently records what it saw; attribution
reconciles the two and **stores which of them it believed**.

```
client says:  "reel R sent me"        →  order.source_reel_id
server saw:   click on action A       →  action_clicks
attribution:  model = last_click | declared | view_through | none
```

---

## 3. The four models

Precedence is strict. Each step is weaker evidence than the one above it, and
the first that matches wins.

| Model | Meaning | Verifiable? |
|---|---|---|
| `last_click` | Server recorded a CTA press by this viewer, inside the window, on something this order actually bought | **Yes** |
| `declared` | Client named a reel; no server-recorded click corroborates it | No |
| `view_through` | Reel for this target was served to the viewer; no CTA was ever pressed | No |
| `none` | Organic — browse, search, repeat order | n/a |

**`view_through` is never folded into `last_click`.** This is the single easiest
way to make a feed look like it works, and it is the one every analytics vendor
does by default.

**One order attributes to at most one reel.** Enforced by a primary key on
`order_attributions.order_id`, not by convention. Without it, an order that
touched three reels contributes three conversions and the funnel sums to more
revenue than the business made.

### 3.1 Windows

| Window | Value | Reasoning |
|---|---:|---|
| Click | 24h | Laundry and meals are same-day or next-day decisions. A viewer who saw a reel Monday and books Friday has almost certainly been re-reached by something else. A 7-day window roughly doubles attributed conversions without doubling anything real. |
| View-through | 4h | A click is a stated intent that survives a night's sleep. A view is not. View-through credit should be the hardest to earn. |

Every attribution stores its own `window_seconds` and `latency_seconds`. The
window is arbitrary and will be retuned; storing it means historical numbers
stay interpretable under the rule that actually produced them, and storing
latency means the retuning can be done against the observed distribution rather
than a hunch.

### 3.2 Off-feed traffic

A vendor pushing their own reel to a WhatsApp group converts at rates the feed
will never approach. That is a real and valuable marketing channel. It is **not**
evidence that the feed works.

`off_feed` is set for any click whose surface is not `feed` or `search`, and the
funnel reports it separately. Folding it into the headline is how a team
convinces itself to keep funding a feed nobody uses.

---

## 4. The denominator problem

"Conversion rate" is meaningless without saying which denominator it used, and
the available ones differ by an order of magnitude:

```
orders ÷ clicks         ~10× flattering
orders ÷ views
orders ÷ impressions    the number egress is actually spent on
```

So there is **no `conversion_rate` field anywhere in this phase.** The API
returns raw counts plus rates named for their denominators
(`orders_per_click`, `orders_per_impression`). A single field forces whoever
renders it to pick one, and the one picked in a board deck is never the
conservative one.

`GET /v1/analytics/feed-funnel` additionally returns a `definitions` block
alongside the numbers, so the figure cannot circulate stripped of what it means.
`feed_share_of_orders` — verifiable click-attributed orders ÷ **all** orders
placed, organic included — is computed server-side so everyone quotes the same
definition.

### 4.1 Impressions had to become durable

Phase 2 deliberately kept the seen-set in Redis and out of the serving path
(§4.2 of that document). That was right for serving and wrong for measurement:
without a durable impression count there is no denominator, and the rate
silently becomes orders-per-click.

`feed_impressions` is therefore written on every feed page, day-partitioned from
the start because it is the highest-volume table in the product. It stores
`position` — rank within the page — because a conversion rate that ignores
position compares a reel shown first against one that was barely shown at all.

---

## 5. The seam, finally load-bearing

The `action` seam has existed since Phase 0 with one stub handler. Phase 3 is
where it does work.

```
reel_actions (kind, target_type, target_id)
      │
      └─► action.Registry ─► handler in the owning module
                              ├─ laundry:  book_service  → service_offering
                              └─ social:   follow, share, open_menu, external
```

The feed resolves every CTA on a page through `ResolveBatch` — two round-trips
for 20 reels, not 40 — and never learns what a laundry offering is.

**`social` exists because not every action belongs to a vertical.** `follow`,
`share`, `open_menu` and `external` target vendors, branches and URLs. Putting
them in `laundry` would mean deleting laundry deletes the follow button; putting
them in `feed` would mean the feed knows what a vendor is.

### 5.1 Handlers do eligibility, not just labels

The Phase 0 laundry handler returned `Available: true` unconditionally. It now
checks that the offering is live, the branch is open, and the viewer's location
falls inside the service area — and returns a **customer-readable reason** when
it is not.

Two reasons this belongs in the feed rather than the booking screen:

- In a feed where the next swipe is free, a customer who taps, waits for a
  screen, and is then told the vendor does not deliver to them is a customer
  lost to a question we could have answered before they tapped.
- A greyed-out button with no explanation reads as a broken app, and the vendor
  gets blamed for a platform decision.

Eligibility errors render **optimistically**. Blanking every CTA on a page
because one query timed out is a far more visible failure than one customer
reaching a screen that says no.

---

## 6. Where attribution runs, and the argument against it

`AttributeOrder` runs **inside the order transaction**.

The case against is real: attribution is analytics, an analytics failure should
never fail a customer's order, and taking a transaction hostage to a measurement
is how measurement starts costing revenue.

The case for wins on one fact: **the evidence is time-windowed, so there is no
backfill.** An order that commits without its attribution row is permanently
unattributable — by the time anything notices, the clicks have aged out and the
answer is gone. And a conversion number with a silent hole in it wherever a
background job failed is exactly the number this phase exists to prevent.

The risk is bounded by a compromise: a resolution *error* is logged and
downgraded to `none` rather than propagated. A broken attribution query costs
accuracy on that order, never the order itself. Only a failure to write the row
at all takes the order with it — and that means Postgres is already refusing
writes, in which case the order's own INSERT is failing too.

---

## 7. API surface

| Method | Path | Notes |
|---|---|---|
| `PUT` | `/v1/reels/{id}/actions` | Replaces the CTA list. Refused once moderation has begun |
| `POST` | `/v1/actions/{id}/click` | Records **and** routes. Public; needs an identity |
| `POST` | `/v1/me/follows` | |
| `DELETE` | `/v1/me/follows/{id}` | |
| `PUT`/`DELETE` | `/v1/reels/{id}/save` | |
| `GET` | `/v1/reels/{id}/funnel` | Vendor-scoped, by authorship |
| `GET` | `/v1/analytics/feed-funnel` | Staff-only. The Phase 3 decision |

Two of these carry a design decision worth restating:

**`POST /actions/{id}/click` records and routes in one call.** If the client
routed itself and reported the click separately, every dropped analytics request
would be a conversion the server never saw — and those drops are not random,
they cluster in exactly the bad-network conditions where conversion is already
lowest. Coupling them means the number cannot quietly under-count.

**The click is recorded even when the CTA resolves unavailable.** A viewer
pressing "Book" on a fully-booked branch is demand, and the most actionable
signal a vendor will ever get. Dropping it because the outcome failed hides a
capacity problem behind a conversion one.

---

## 8. Exit criteria

Phase 3 is done when all of these are true:

1. A reel in the feed shows a CTA whose label, availability and reason come from
   the owning vertical's handler.
2. Tapping it records a click and routes to booking.
3. Completing that booking produces an `order_attributions` row with
   `model = 'last_click'` and a latency consistent with the session.
4. `GET /v1/analytics/feed-funnel` returns impressions, clicks and orders split
   by model, over a date range, with definitions attached.
5. A vendor can see their own reel's funnel and **cannot** see anyone else's.
6. An order placed by browsing — never touching the feed — records
   `model = 'none'` rather than being credited to a reel the user happened to
   scroll past.
7. Someone has looked at criterion 4's output and stated, in writing, whether
   the feed is producing bookings.

Criterion 7 is the point of the phase. The other six are plumbing for it.

---

## 9. Implementation status

**Built and unit-tested**

| Area | Where |
|---|---|
| Schema | `migrations/000013_actions_attribution.*` |
| Attribution rules | `internal/modules/attribution/attribution.go` — pure function, 18 tests |
| Storage & evidence | `internal/modules/attribution/repository.go` |
| Funnel queries | `internal/modules/attribution/funnel.go` |
| CTA storage & clicks | `internal/modules/actions/` |
| Platform action kinds | `internal/modules/social/` |
| Laundry eligibility | `internal/verticals/laundry`, backed by `internal/modules/catalog` |
| Shared seam wiring | `internal/app/verticals.go` — one registration path for all three binaries |

**Deviations from the plan text**

1. **`feed_impressions` is back.** Phase 2 called it "intentionally absent from
   serving" and it still is — nothing on the read path queries it. But it had to
   become durable, because it is the denominator (§4.1).
2. **`vendor_users` and `platform_admins` arrived in Phase 2**, not here. Phase 3
   assumes both.
3. **`AuthoredByOrderedVendor` on `Evidence` is unused in Phase 3.** It is
   populated by nothing and read by nothing, and exists because Phase 6 adds
   user-authored reels, where a reel by a third party pointing at a vendor is a
   materially different claim. Flagged here so it is not mistaken for dead code —
   though if Phase 6 slips, it *is* dead code and should be deleted.

**Not verified.** The migrations have never been applied. The funnel queries in
particular — lateral joins, `FILTER` aggregates, a partitioned table — have
never been parsed by Postgres.
