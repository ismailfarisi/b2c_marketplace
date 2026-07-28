# Phase 6 — Vehicles

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 6 and [SCOPE-EXTENSIONS.md](./SCOPE-EXTENSIONS.md) §3
**Scope:** listings, vehicle taxonomy, seller reels, chat, offers, listing packages, fraud tooling.
**Out of scope:** test-drive booking (Phase 7), `market_valuations` (§6), Vehicle Gate integration (§6), escrow (§5), dealer console (Phase 7).

Phase 5 tested the **downstream** seam — could a new vertical be added to
`commerce` and `scheduling` by registering strategies rather than editing
internals? This phase tests the **upstream** one. PLAN-VIDEO-FIRST §2 states it
as a sentence:

> Where the driver app never learns what a meal is, the feed never learns what a
> car is.

Everything below follows from taking that literally, and from taking
SCOPE-EXTENSIONS §3's opening claim literally too: *"your entire money module
does not apply."*

---

## 1. What this vertical is not

A used car binds **none** of the four service-type strategies. Nothing is
fulfilled, nothing is scheduled, no capacity is drawn, and the price is agreed
at an RTA counter the platform never sees. So `vehicles` does **not** call
`servicetype.Register`. There are no stub strategies.

That was a deliberate refusal. Registering four no-op strategies would have made
the wiring symmetrical and the model a lie — the first person to read
`vehicles.fulfillmentPattern` would reasonably conclude a car can be ordered.
Phase 5's meals vertical registers two service types because a meal box genuinely
is fulfilled; a car genuinely is not.

The seam the vertical *does* use is one line:

```go
actions.Register(action.KindInquireListing, action.TargetListing,
    vehicles.NewInquiryHandler(pool))
```

---

## 2. The gate: did the action seam hold?

**Yes — and it is now enforced by a test rather than by this paragraph.**

The claim is falsifiable in exactly one way: something in the core imports a
vertical. That is not a big diff. It is one import added eighteen months from
now by someone who needs a listing's price on a feed tile and reaches for the
obvious package. A prose assertion in a phase document does not survive that.

So `internal/app/seam_test.go` parses the imports of fifteen core packages —
`feed`, `ranking`, `moderation`, `reels`, `media`, `comments`, `interests`,
`engagement`, `social`, `search`, `actions`, `attribution`, and all three seams —
and fails if any of them names `vehicles`, `chat`, `classifieds`, `trust`,
`subscriptions`, or `internal/verticals/`. A companion test asserts the positive:
`app` — whose entire job is to know the whole graph — *does* import them, so a
future refactor that "fixes" the first test by pushing registration into each
module gets caught by the second.

What Phase 6 actually cost the core:

| Core package | Change |
|---|---|
| `feed` | none |
| `ranking` | none |
| `moderation` | none |
| `media` | none |
| `search` | none |
| `seam/action` | none — `KindInquireListing` and `TargetListing` were declared in Phase 3 |
| `reels` | **one interface**, `SubjectAuthorizer`, and one method that uses it |
| `resolve_reel_coverage` | one `ELSIF` branch, in SQL |

### 2.1 The one core change, and why it is not a leak

`reels.RequestUpload` authorises through branch membership. A person is not a
member of anything, so a user-authored reel's authority has to come from
somewhere else — and the only defensible answer is **owning the subject**. Hence:

```go
type SubjectAuthorizer interface {
    AuthorizeSubject(ctx context.Context, userID uuid.UUID,
        subjectType string, subjectID uuid.UUID) error
}
```

`reels` learns that some subjects have owners. It does not learn that some
subjects are cars. The one implementation lives in `vehicles`, and `app` wires
them together. If a second kind of user-authored content ever exists this becomes
a map keyed on subject type — and not before, because a registry with one entry
is a registry that has never been tested with two.

`RequestUserUpload` is a separate endpoint from `RequestUpload` rather than a
nullable `branch_id`. The two look similar and authorise on completely different
grounds; one endpoint choosing between them based on which field is null is one
refactor away from letting a user post as a vendor.

### 2.2 Coverage: the extension point Phase 2 left open

A seller reel's audience is a circle around the car, not a service-area polygon.
Phase 2 wrote `resolve_reel_coverage` with an `IF v_author_type =
'vendor_branch'` and no `ELSE` — that shape was the extension point, and this is
the phase that used it. Migration 000016 `CREATE OR REPLACE`s the function with a
`user` branch that buffers the listing's point by its radius.

No Go changed. No feed query changed. No index changed. The feed still selects on
`coverage` and still does not know what a car is.

The `user` branch refuses when the reel has no subject, and that refusal is
load-bearing: it is what keeps general UGC out of the feed until a phase
deliberately admits it. Content with no subject also has no coverage geometry, so
it would publish to nobody while still costing a human review.

### 2.3 Where the seam *did* need widening — `payments`

`payments.order_id` was `NOT NULL`. That encoded an assumption which held for
five phases: every payment settles an order, and every order has a vendor, a
branch and a service type. **A listing fee has none of those.** The platform is
selling something itself for the first time.

Both alternatives were worse:

- A synthetic "platform vendor" row makes every payout run, capacity query and
  commission report carry an exclusion it will eventually forget.
- A second payment table forks the gateway integration, the webhook handler and
  the reconciliation surface in two.

So the abstraction got fixed instead:

```sql
ALTER TABLE payments ALTER COLUMN order_id DROP NOT NULL;
ALTER TABLE payments ADD COLUMN package_purchase_id uuid REFERENCES package_purchases (id);
ALTER TABLE payments ADD CONSTRAINT payments_exactly_one_subject
    CHECK (num_nonnulls(order_id, package_purchase_id) = 1);
```

This is PLAN.md §5 P2's gate language applied one phase late: *stop and fix the
abstraction*. `payments` was named for what it is and coupled to what it happened
to be used for. **`orders` turned out to be a commerce table, not a billing one** —
that is the sharpest finding of the phase and it will matter again the first time
the platform sells anything else directly.

---

## 3. The money, precisely

SCOPE-EXTENSIONS §3 says the money module does not apply. That is close but not
exact, and the exact version is worth having:

| | Services / meals | Classifieds |
|---|---|---|
| Ledger | applies | **applies unchanged** |
| Commission model | applies | **does not apply** — no GMV to take a percentage of |
| `vendor_payable` | credited on capture | **never touched** — not "zero", *no posting* |
| `orders` | the billing object | **does not fit** |
| Supplier of record | the vendor | **the platform** |
| VAT timing | at recognition | **at charge** |

Three events, in `internal/modules/ledger/listings.go`:

```
charge      Dr gateway receivable    Cr deferred revenue     cash in, nothing earned
                                     Cr VAT payable
recognise   Dr deferred revenue      Cr platform revenue     earned, per day of the window
refund      Dr deferred revenue      Cr customer wallet      unearned portion returned
            Dr VAT payable
```

**VAT is booked at charge, not at recognition, and that differs from
`PostSubscriptionRecognised` on purpose.** In a subscription the vendor is the
supplier of record and the platform's VAT is on its own commission, earned as
meals are delivered. Here the platform *is* the supplier, and UAE VAT on a supply
is due at the earlier of invoice or payment — the moment the seller pays for the
package, regardless of how much of the thirty days has run. Deferring it to match
revenue recognition would understate output tax on every return filed mid-window.

### 3.1 The one thing Phase 5 built that turned out to be universal

`RecogniseThrough` — the cumulative-floor arithmetic that stops fils being
stranded in deferred revenue — moved from `subscriptions` to `internal/money`.

A prepaid meal cycle recognised across its delivery days and a prepaid listing
package recognised across its thirty-day window are the same problem with
different nouns. `subscriptions` now delegates and keeps its wrappers, so every
Phase 5 call site and all nineteen of its billing tests are untouched and still
pass. The classifieds test re-asserts the property for a different noun: 42
amount/duration combinations, recognised day by day, must sum to **exactly** the
amount charged.

It did *not* try to reuse `subscriptions` wholesale — see §7.

---

## 4. Trust & safety is the phase, not a section of it

PLAN-VIDEO-FIRST §9 ranks "T&S underestimated" as a serious risk and names this
phase as when it stops being manageable by one part-time reviewer. Two things
change at once: content moves from vendor-authored to user-authored, and the
category itself is defined by fraud.

### 4.1 The moderation volume step change

Every seller reel author is `new` in `author_trust`, so `moderation.Route` sends
**100% of it** to a human before publish. That is correct and it is expensive.
Nothing in the code mitigates it and nothing should — the mitigation is headcount,
which the plan already says. What the code does is set the seller's expectation at
the point of upload, because a seller who expects their video live in ninety
seconds and gets it in two hours writes a one-star review about a system working
exactly as designed.

Phase 6 adds a **third** moderation queue (listings) and a **fourth** (held chat
messages) alongside reels and comments. Each has its own, for the reason Phase 4
gave comments theirs: different arrival rate, different judgement time, and
interleaving buries the queue with a publish SLA on it.

### 4.2 The chat scanner

Chat exists so the phone number never becomes public. That makes
`internal/modules/chat/scan.go` load-bearing — a chat that passes a number through
in the first message has created the fraud surface without the protection, and
would be worse than publishing the number honestly.

Three decisions in it are worth defending:

**Arabic-Indic digits are folded before matching.** A phone number typed on an
Arabic keyboard is `٠٥٠١٢٣٤٥٦٧`, and a scanner written against `[0-9]` sees no
digits in it at all. This is not an edge case in this market; it is how a large
share of users type, and it is what every determined evader would discover within
a week. The fold is rune-for-rune so a match found in the folded text has the same
offsets in the original — working in bytes would misplace every redaction in an
Arabic message. Extended Arabic-Indic (`۰۱۲۳`), non-breaking spaces and the RTL/LTR
marks a phone keyboard inserts around Latin digits are all handled for the same
reason.

**Phone detection is by shape, not by length.** "Nine or more digits" is the
obvious rule and it breaks the product: a car chat is full of long numbers, and
`AED 1,450,000` on a 2021 model with 95,000 km is three of them in one sentence.
Redacting the price destroys the conversation the product exists to enable. So the
rule matches Gulf country codes, local trunk formats, bare UAE mobiles and
explicit `+` — a price never matches any of them. There is a test asserting
exactly this, with five real price/mileage/VIN sentences.

**Contact details are redacted; advance-fee language is held.** These are
different failure modes and they need different responses. Most people sharing a
number are not defrauding anyone — they are arranging a viewing and the app felt
slower than WhatsApp; blocking them teaches them the product is broken. But
stripping "Western Union" from a scam leaves a fluent message that is still a
scam, so those go to a person. A held message does **not** bump the recipient's
unread badge: a badge with nothing behind it reads as a bug and produces a support
ticket for every held scam.

Releasing a held message delivers the **redacted** body, never the original.
Judging that a message is not a scam is not the same decision as publishing the
phone number inside it, and only one of them was asked for.

### 4.3 The trust score, and the bug the tests caught

Same shape as Phase 4's interest profiles: append-only signals, folded with
exponential decay into a derived score that is never incremented in place. Here
the stakes are higher — this score decides whether a seller can trade.

The weights table has to satisfy two invariants that pull in opposite directions:

1. one **confirmed** top-tier signal must block a seller on its own
2. no **open** signal of any kind may block a seller on its own

The first draft had `stolen_vehicle` at 90 points, which satisfied (1) and quietly
broke (2): a single unreviewed detector hit scored 10 and blocked the account
outright. `TestNoMachineSignalCanBlockOnItsOwn` caught it, and the fix was to cap
every weight below `100 − BlockedBelow` and let the confirmed multiplier do the
blocking.

The resulting property is the safety guarantee of the whole module and is worth
stating plainly: **a machine cannot end an account. Only a human confirming what
the machine found can.** The test runs over every key in the map rather than a
sample, because this invariant is violated by *adding* a weight, not by editing
an existing one — which is precisely the change nobody thinks to test.

Two more choices:

- **Additive penalties, not a product of probabilities.** The product is more
  defensible statistically and useless in practice: nobody can explain to a
  suspended seller why 0.94 × 0.88 × 0.91 crossed a line, and every appeal becomes
  a research project. `Explain()` returns the same arithmetic the decision used —
  not a reconstruction, which would drift the first time either was touched, and
  the drift would surface in front of a suspended seller.
- **A user's score reads their listings' signals too.** A seller with five
  listings each carrying one mild signal is the pattern being looked for; scoring
  the account in isolation from its inventory misses it entirely because each
  listing looks fine and the *account* is the fraud.

Dismissed signals are kept at zero weight. "We looked and it was fine" is a
different fact from "we never saw it", and only one of them survives an appeal —
or stops the next detector run raising it again forever.

### 4.4 Detectors

Split by "does this need a second row". Inline at authoring: odometer against
model year, VIN check digit. In the worker: everything cross-listing.

**Odometer rollback** is the one with unambiguous evidence — an odometer does not
decrease. Severity scales with the size of the reversal, because 3,000 km is a
mistake and 90,000 km is a decision. Only the platform's own history is visible; a
rollback done before the car was ever listed here is invisible without a Vehicle
Gate integration.

**The VIN check digit is a signal, never a validator.** It is mandatory only for
vehicles built for North America, so genuine GCC-spec and Japanese-import VINs
fail it routinely — refusing those listings would delete most of the market. What
it *is* good for is the other direction: a fabricated VIN, seventeen plausible
characters typed to make a listing look documented, passes with probability about
1 in 11. Combined with `regional_spec = 'us'`, a failure is strong evidence. There
is a test asserting that a well-formed VIN failing the check digit still validates
cleanly through `Draft.Validate`.

**Mileage plausibility uses a GCC baseline.** 22,000 km/year, not the ~12,000 a
European dataset would give: long commutes, cheap fuel, intercity driving that
would be a flight elsewhere. A European benchmark would classify a large share of
honest Dubai listings as high-mileage and the band would stop meaning anything.
There is a test guarding the constant itself.

---

## 5. What cannot be measured here — read before quoting any number

Phase 3 §4 carries the same warning for orders. This vertical's version is worse
and it is structural.

**The funnel ends at the lead.** `POST /listings/{id}/inquire` is this vertical's
conversion event — the analogue of `POST /orders` — and it is where verifiable
data stops. The sale happens at an RTA counter. The platform does not see it,
cannot see it, and will not see it under any architecture short of becoming a
party to the transaction.

Concretely:

- `vehicle_listings.sold_price_minor` is **seller-declared**. It is a survey
  response, not a market observation. The API says so in the response body and the
  column carries a `COMMENT` saying so.
- A listing marked sold may have sold elsewhere, or not at all.
- A listing never marked sold may well have sold — sellers stop caring once the
  money arrives.
- Therefore **conversion rate from lead to sale is not computable.** Anyone
  quoting one has estimated it, and the estimate is unfalsifiable.

What *is* honest: impressions → clicks → leads, all observed, all attributable
through the Phase 3 chain. `listing_leads.source_reel_id` makes "did a reel
produce a lead" answerable with the same query shape as "did a reel produce an
order", which is the number this whole plan exists to produce.

**Offers create no obligation.** No payment intent, no hold, no ledger entry.
Escrow would solve most of this category's fraud and implies a payment-institution
licence (SCOPE-EXTENSIONS §3), which is a decision nobody has taken. The API
returns `binding: false` on both the offer and the response to it, so a client
cannot render an accepted offer as a commitment without ignoring the payload.

---

## 6. What was deliberately not built

**`market_valuations`.** SCOPE-EXTENSIONS §3 calls "what's this car worth" the
highest-value feature in the category, and it is right. It is still deferred, for
a reason specific to this architecture rather than to scope: **we never observe a
sale price.** A valuation table built from this data would be a valuation of
*asking* prices, and it would be wrong in a knowable direction — high. Shipping
that as "market value" would be worse than shipping nothing, because sellers would
anchor on it and buyers would cite it.

Price-outlier detection still works, because it does not need a true valuation:
comparing a listing against the median asking price of its comparables catches the
bait pattern regardless of whether that median is the true market. The detector
requires five comparables so a rare trim's only listing is not flagged against
nothing.

**`listing_videos`.** Reels already are the listing's video. A second video table
would fork the transcode and moderation pipeline, which is precisely the reuse
this phase is built on.

**Test-drive bookings.** SCOPE-EXTENSIONS calls this "one genuine synergy" —
`fulfillments` + `capacity_buckets` unchanged — and it is not in PLAN-VIDEO-FIRST
§8's deliverable list for this phase. It also needs a dealer console to be worth
anything, which is Phase 7. Deferred to where it belongs rather than smuggled in
because the abstraction happens to fit.

**Escrow, and Vehicle Gate / Absher integration.** The first is a licensing
decision, the second is the category's real moat and needs a commercial agreement
before it needs code. `vehicle_history_checks` is shaped for that integration and
is useful before it exists — a manually keyed report from an authorised centre
lands in exactly the same row.

---

## 7. Where a duplication was accepted, and the exact diff that would remove it

`dealer_subscriptions` duplicates machinery Phase 5 already built. `subscriptions`
has cycles, dunning, deferred revenue, proration and a state machine, and all of
it applies to a recurring dealer package.

It was not reused because **`subscription_plans` is bound to a vendor branch and a
service type.** Phase 5 built recurrence as engine code — correctly, and this
phase confirms that was the right call — then tied it to the *fulfillment* axis. A
dealer package delivers no fulfillments and has no branch.

The unification is not large and is written down here so it does not have to be
rediscovered:

1. Split `subscription_plans` into a **billing plan** (`cycle_days`, price,
   currency, tax, dunning policy) and a **delivery plan** (`branch_id`,
   `service_type`, `menu_cycle_id`, `days_per_week`, `meals_per_day`,
   `cutoff_hours`), with the second optional.
2. Drop `subscriptions.branch_id`'s NOT NULL and make the scheduler a no-op when
   there is no delivery plan.
3. Replace `dealer_subscriptions` with a subscription whose delivery plan is null
   and whose renewal opens a `package_purchases` row instead of an `orders` row.

That is a change to a live launch vertical's core table and it does not belong in
a phase whose job was to prove the *other* seam. Doing it here would have meant
touching meals to ship cars, which is the exact failure the gate exists to detect.

---

## 8. Known limitations

Surfaced rather than fixed, in rough order of how likely each is to bite.

1. **Listing quota is checked at submit and spent at approval.** A seller with one
   credit can submit three listings; the third approval fails on the database
   CHECK. The failure lands on a **moderator**, which is the wrong person. Closing
   it needs a reservation table, which was judged not worth it at launch volume —
   but the wrongness is in the code comment, not just here.
2. **`vehicles.Search` is Postgres, not OpenSearch.** Correct for exact facets and
   completeness, which is what car search needs. It becomes wrong when free text
   over descriptions matters more than exactness. The trigram index covers the
   escape hatch, not the main path.
3. **Trust rescoring is polled, not event-driven.** `RescoreDirty` picks up
   anything not recomputed in 24 hours, which is what makes decay actually happen —
   scores decay with time and nothing writes a row when that occurs. The cost is
   that a confirmed signal takes up to one tick (10 minutes) to restrict a listing.
   The `Decide` endpoint rescores synchronously to cover the case that matters.
4. **`seller_verifications` has no provider behind it.** The table and the
   `unverified_high_value` detector are live; nothing issues a verification. Until
   a KYC provider is wired (Phase 8), every private seller is unverified and that
   signal fires on every high-value listing — it is weighted at 15 points precisely
   so that is survivable, but it will be noise until then.
5. **`listing_photos` is written by nothing.** The table, the moderation state and
   the pending index exist; there is no upload endpoint for stills. Seller reels
   went first because they are what the feed needs. Photos are a Phase 7 client
   concern.
6. **No dealer console.** Dealers can buy packages and list cars through the API;
   there is no interface for bulk upload or lead management, which is what actually
   sells dealer packages. Phase 7.
7. **`vehicle_trims` and `vehicle_specs` are seeded with nothing.** The taxonomy
   seed covers 12 makes and 19 models — enough to run one city. Trims are licensed
   reference data (SCOPE-EXTENSIONS §3) and a hand-maintained trim table goes stale
   in one model year; every stale row is a listing that cannot be filtered.
8. **The chat scanner does not catch numbers spelled as words** ("zero five zero
   one two three"), homoglyph substitution, or a number inside an uploaded image.
   The first two are cheap to add when they appear; the third is why
   `message_attachments` has a moderation state.

---

## 9. What shipped

**Migration `000016_vehicles`** — 25 tables:

| Group | Tables |
|---|---|
| Taxonomy | `vehicle_makes`, `vehicle_models`, `vehicle_trims`, `vehicle_specs` |
| Sellers | `dealer_profiles` |
| Listings | `vehicle_listings`, `listing_photos`, `listing_status_history`, `listing_price_history` |
| Demand | `listing_leads`, `listing_offers`, `saved_searches`, `saved_search_alerts`, `listing_favorites` |
| Chat | `conversations`, `messages`, `message_attachments` |
| Trust | `seller_verifications`, `vehicle_history_checks`, `inspection_reports`, `fraud_signals`, `trust_scores` |
| Money | `listing_packages`, `package_purchases`, `listing_promotions`, `dealer_subscriptions` |

That is 26 by the row count above and 25 against SCOPE-EXTENSIONS' budget:
`listing_videos` was dropped (reels already are the video) and `dealer_profiles`
added (a dealer is not a vendor and had nowhere to live). `market_valuations` is
deferred per §6.

Plus: the `payments` widening (§2.3), `resolve_reel_coverage` extended for user
authors, `generate_listing_reference`, and a taxonomy + packages seed.

**Go**

| Package | What it is |
|---|---|
| `modules/vehicles` | listing status machine, VIN, mileage bands, faceted search builder, repository, service, the inquire CTA, the subject authorizer |
| `modules/chat` | the message scanner (pure), conversations, held-message moderation |
| `modules/trust` | signal weights, the decaying fold, `Explain`, five SQL detectors, the reviewer queue |
| `modules/classifieds` | package entitlements (pure), purchase, quota consumption, bumps, features, recognition |
| `modules/ledger/listings.go` | three postings, none of which touch `vendor_payable` |
| `money/recognition.go` | the Phase 5 arithmetic, made vertical-neutral |
| `api/vehicles.go`, `api/chat.go` | 22 endpoints |
| `cmd/worker/vehicles.go` | six jobs on a 10-minute tick |
| `app/seam_test.go` | the gate, mechanised |

**Tests** — 19 packages passing. New: 21 in `vehicles`, 14 in `chat`, 16 in
`trust`, 15 in `classifieds`, 2 in `app`.

**Verified:** `gofmt`, `go vet`, `go build`, `go test -count=1 ./...` all clean;
`redocly lint` valid with 23 cosmetic warnings.

**Not verified.** The migrations have never been applied. This one adds 25 tables,
two `CREATE OR REPLACE FUNCTION`s, a `gin_trgm_ops` index, a partial unique index
used for `ON CONFLICT` inference in six statements, and an `ALTER` that drops a
`NOT NULL` on a Phase 1 table. The five fraud detectors in particular are large
`INSERT … SELECT` statements with window functions and ordered-set aggregates that
no Postgres has parsed — one of them (`percentile_cont … OVER`) was already found
to be invalid by reading rather than by running, which is not a method that
scales.
