# Phase 5 — Food Vertical & Subscriptions

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 5 and [PLAN.md](./PLAN.md) §5 P2
**Scope:** meal plans, menu cycles, the rolling scheduler, prepaid billing with deferred revenue, pause/skip/cancel, dunning.
**Out of scope:** vehicles (Phase 6), stored cards and the actual retry charge (Phase 8), the vendor kitchen console (Phase 7).

PLAN.md §5 P2 calls this phase **the architecture's exam** and writes the gate
down in one sentence:

> *If adding meals required changing `commerce` or `scheduling` internals rather
> than adding strategies, stop and fix the abstraction.*

§2 is the result. The rest of this document is what got built around it.

---

## 1. What shipped

| | |
|---|---|
| Meal catalogue | `menu_items`, `menu_cycles`, `menu_cycle_days`, `menu_day_items`, `subscription_plans`, `plan_tiers` |
| Recurrence engine | `subscriptions`, `subscription_cycles`, `subscription_schedule_rules`, `subscription_pauses`, `subscription_skips`, `subscription_modifications`, `subscription_meal_selections` (partitioned), `subscription_events` |
| The calendar Phase 0 promised | `branch_calendar_days` |
| Vertical | `internal/verticals/meals` — four strategies, two service types, no other logic |
| Scheduler | `internal/modules/subscriptions/schedule.go` — pure, 24 tests |
| Billing | `billing.go` + `internal/modules/ledger/subscriptions.go` — 19 tests |

Fifteen tables. DATA-MODEL.md budgeted exactly that: six in §3 Catalog and seven
in §6 Subscriptions, plus the calendar and the events log.

---

## 2. The gate: meals passed, recurrence was missing

**A meal box ordered for tomorrow fits the Phase 1 engine with zero engine
changes.** `meals_one_off` registers four strategies and that is the entire
diff. It produces one fulfillment where laundry produces two, draws
`kitchen_covers` where laundry draws `pickup_slot`, prices final-at-booking
where laundry prices provisionally. Nothing in `ordering`, `capacity` or
`pricing` was touched. `TestRegisteringMealsCostsNoEngineChanges` executes that
claim rather than asserting it.

**A meal *subscription* does not fit, and not because meals are special.**

Nothing in the engine modelled **recurrence**. `ordering.PlaceOrder` produces
one order with N fulfillments on dates the customer picks at checkout. A
subscription produces recurring billing periods, a rolling window of
fulfillments materialised over weeks, and mutations — pause, skip, plan change —
that arrive after the money did. A weekly laundry pickup would have hit exactly
the same wall.

So the honest reading of the gate is: **the vertical seam held; the phase
revealed a second axis the plan had not named.** Recurrence is orthogonal to
vertical, and `internal/modules/subscriptions` is engine code that happens to be
needed first by food. Nothing in that package mentions a meal. Laundry could
subscribe tomorrow.

Two smaller findings came out of the same exam:

### 2.1 The action seam's key was one level too coarse

Phase 3 let `laundry` register the `(book_service, service_offering)` CTA
handler because laundry was the only vertical. **A meal box is also a service
offering.** The second registration would have panicked at boot on a duplicate —
the registry working correctly and telling us the ownership was wrong.

The fix keeps the seam rather than widening the key: the pair is owned once by
`catalog`, which owns the table, and each vertical contributes only
`action.Labels` — words and a route. Verticals still import nothing but the
seams. `TestVerticalLabelsDoNotCollide` guards it.

### 2.2 `subscription_plans` is a catalogue table, not a subscriptions table

DATA-MODEL.md §3 always said so. It matters because it decides where the
`subscribe_plan` CTA lives, and getting it wrong would have made `meals` import
a database.

---

## 3. The rolling scheduler

PLAN.md §4: *"A subscription does not pre-generate a year of fulfillments. A
nightly job materializes a rolling 14-day window."*

`Plan(sub, rules, pauses, skips, from, to, now) []Occurrence` is pure. Every
hard part of scheduling is a date-arithmetic decision, and those belong
somewhere a test can reach them without a database and without waiting a day.

It **describes desired state rather than a change**, which is what makes it
idempotent — and idempotence is what lets the nightly job, a manual retry and a
resume-after-payment all call it without coordinating. Three layers of belt and
braces: the planner is idempotent, the insert is `ON CONFLICT DO NOTHING`, and a
unique index on `(subscription_id, scheduled_date, sequence)` makes a
double-write impossible even if the first two were wrong.

Five decisions inside it are worth more than the rest:

**Dates are local, not UTC.** At 03:00 in Dubai it is 23:00 UTC on the *previous
day*. Truncating in UTC shifts every early-morning materialisation back a day
for the entire Gulf. `dayIn(t, loc)` is the most important function in the file
and `TestDatesAreLocalNotUTC` is the reason it stays that way.

**Weekday 0 is Sunday.** The GCC working week starts Sunday and Go's
`time.Weekday` agrees. ISO-8601 does not. Mixing them delivers every meal one
day late — invisible in review, extremely visible to the customer.

**Skips do not renumber the remaining slots.** `sequence` is the uniqueness key
on `fulfillments`. If skipping lunch promoted dinner from sequence 2 to 1, the
next run would see sequence 2 missing and create a duplicate dinner.

**The planner cannot describe the past.** PLAN.md §4 requires that pauses and
skips never touch past fulfillments. Rather than checking that at each call
site, `Plan` clamps its own start to today — the guarantee is structural.

**The cutoff is money.** Past it the kitchen has bought the ingredients and often
cooked them, so a late skip is waste the vendor absorbs. `CanModify` refuses
*exactly at* the boundary: a customer who is one second late and gets their skip
accepted, then is charged, is a support ticket; one who is refused sees a
message.

---

## 4. Prepaid money, and the fils that go missing

PLAN.md §6: *"bill the full cycle up front, recognize revenue daily in the
ledger."*

```
charge      Dr gateway receivable   Cr deferred revenue     cash in, nothing earned
recognise   Dr deferred revenue     Cr vendor payable       earned, per delivery day
                                    Cr platform revenue
                                    Cr VAT payable
credit      Dr deferred revenue     Cr customer wallet      paused, owed back
```

`deferred_revenue` and `customer_wallet` have been in the `ledger_account_type`
enum since migration 000001 with nothing posting to them. This is the phase that
finally does.

**Why it cannot be collapsed into "book it all as revenue on charge":** a
vendor's payable would jump by a full cycle the moment a customer signs up, and
a payout run the next day would pay them for four weeks of meals they have not
cooked. Recovering that from a vendor who then cancels is a conversation nobody
wins.

### 4.1 Recognition is a cumulative floor, not a daily rate

The obvious implementation — `floor(amount/days)` posted `days` times — loses up
to `days - 1` minor units. On a 449.99 AED cycle over 20 days that is 19 fils
permanently sitting in deferred revenue with no event that will ever clear it.
Multiplied across every subscription, it is a liability account that never
reaches zero and that nobody can explain.

`RecogniseThrough` takes the cumulative floor and subtracts what is already
recognised, putting the remainder on the last day.
`TestRecognitionSumsExactlyToTheAmountCharged` runs 42 combinations of awkward
amounts and day counts and asserts the total is exact, not close.

### 4.2 A pause credits only unearned money

`PauseCredit` is bounded by `amount - recognised`. Crediting revenue already
earned by delivering meals is a **refund** — a different event, with different
accounting and a different approval path, that reverses the vendor's payable
rather than the platform's deferred balance.

Credits go to the wallet, not the card: a paused customer is not leaving, and in
this region a card refund can take a week to appear — which reads as the
platform having taken the money twice.

### 4.3 Proration is per remaining day

Charging the new plan's full price and crediting the old one's would let a
customer upgrade on the last day of a cycle and get four weeks of the better
plan for one day of it.

---

## 5. Dunning: nobody finds out at lunchtime

PLAN.md §6 sets the constraint in one clause — *"Meal customers must know
**before** the delivery doesn't arrive"* — and every number follows from it.

- **Renewals are billed 3 days early.** A renewal charged on the last day of a
  cycle leaves the retry schedule no room to run before the next cycle's first
  meal is due.
- **Retries are front-loaded**: 4h, 20h, 2d, 4d. Three attempts inside the first
  two days, while a notification can still reach someone and be acted on.
- **Two days of grace after the final failure.** Two lunches given away rather
  than a subscriber lost. A cycle is weeks of revenue; two meals is not.
- **Then suspension — never deletion.** The rules, the address, the meal history
  and the cycle record all survive. A customer who fixes their card is resumed
  rather than re-onboarded.

The charge itself is not attempted yet: that needs a stored card, which is Phase
8. What exists now is the state machine, so the suspension, grace and
notification hooks are real from the day meals ship rather than added after the
first customer silently stops being fed.

---

## 6. Ramadan is a schema decision, not a feature

PLAN.md §12 lists it as a first-class scheduling concept. Two structural
provisions rather than a Ramadan mode:

- `suhoor` and `iftar` are ordinary values in the `meal_slot` vocabulary, and
  `slotOrder` places them **where they fall in the day** rather than appending
  them. During Ramadan they *are* the schedule; sorting them last produces a
  delivery sheet in the wrong order for a month.
- `branch_calendar_days` — the table `branch_hours` has had a comment pointing at
  since migration 000003 — overrides operating hours per date with an explicit
  `ramadan` kind.

A Ramadan schedule is therefore a rule change and a calendar row, not a code
change. Full seasonal menu switching is Phase 8.

---

## 7. Known costs, stated

**`catalog.BookingHandler` does a second query per CTA** to resolve which
vertical an offering belongs to. On a 20-reel page with 3 actions each that is up
to 60 primary-key lookups — exactly the N+1 the eligibility check was written as
one query to avoid. It is acceptable only because Postgres serves it from cache,
and it belongs in the eligibility query or in `ResolveBatch` the moment CTAs have
real volume.

**Subscription capacity reservations do not fail closed.** A one-off order that
cannot get capacity must fail — the customer is standing there and can pick
another slot. A subscription delivery that cannot get capacity is a meal the
customer has *already paid for*, so the fulfillment is created anyway and the
vendor sees it over their stated limit. That is an operational problem a human
can solve on the day; the alternative is a customer who paid for twenty meals,
received nineteen, and has no record of the missing one.

**The billing-order INSERT is duplicated** from `ordering.Repository`. Calling
`PlaceOrder` would mean adding a "but not for subscriptions" branch to line
validation, fulfillment planning, capacity reservation and attribution — which
is precisely the engine surgery the gate exists to prevent. Fifteen lines of
duplication is the cheaper side of that trade, and it is flagged here so it is
not mistaken for an oversight.

**Recurring jobs run every 15 minutes, not nightly**, despite PLAN.md §4 saying
"nightly". A single nightly run has one unacceptable failure mode: if it fails,
nobody eats tomorrow and the next attempt is in twenty-four hours. The job is
idempotent and returns no rows when there is no work, so running it often is
free.

---

## 8. Exit criteria

1. A customer subscribes from a reel, pays the billing order, and fulfillments
   appear for the next 14 days.
2. Pausing over a weekend on a Sunday-to-Thursday plan consumes **zero** pause
   days and credits nothing.
3. Pausing a working week extends the end date by five delivery days, or credits
   the wallet — and the ledger balances either way.
4. `deferred_revenue` for a cycle reaches exactly zero when its last meal is
   delivered. Not approximately zero.
5. A failed renewal produces retries, then grace, then suspension — and the
   subscription row, its schedule and its history all survive.
6. A skip after cutoff is refused with a message a customer can act on.
7. Adding meals required **no changes** to `ordering`, `capacity` or `pricing`.
   Verified by reading the diff, not by asserting it.
8. Someone has stated in writing whether §2's finding — recurrence as a missing
   axis — was a gap in the plan or a gap in the build.

---

## 9. Implementation status

**Built and unit-tested**

| Area | Where | Tests |
|---|---|---|
| Schema | `migrations/000015_meals_subscriptions.*` | — |
| Rolling scheduler | `internal/modules/subscriptions/schedule.go` | 24 |
| Prepaid arithmetic & dunning | `internal/modules/subscriptions/billing.go` | 19 |
| Subscription lifecycle | `internal/modules/subscriptions/{service,materialise,repository}.go` | — (SQL) |
| Deferred-revenue journals | `internal/modules/ledger/subscriptions.go` | — |
| Meals vertical | `internal/verticals/meals/` | 14 |
| Shared CTAs | `internal/modules/catalog/{booking,plan}_cta.go` | — |
| Recurring jobs | `cmd/worker/subscriptions.go` | — |

**Deviations from the plan text**

1. **Recurrence became an engine module, not part of the vertical.** §2. The
   plan framed Phase 5 as a test of the vertical seam; it was, and it also found
   a seam that did not exist.
2. **The action seam's CTA ownership moved.** §2.1. A Phase 3 shortcut that the
   second vertical would have crashed on.
3. **Jobs are 15-minutely, not nightly.** §7.
4. **`subscription_modifications` is written but nothing writes to it.** The plan
   change path — proration arithmetic and all — is implemented and tested in
   `Proration`, and the API endpoint that would call it is not built. Flagged
   here so it is not mistaken for dead code; if Phase 7's vendor console slips,
   it *is* dead code.

**Not verified.** The migrations have never been applied. Fifteen now. The
queries most worth distrusting, in order: the renewal statement (a CTE inserting
into two tables with a correlated `generate_series` count), `LoadPlan`'s tier
overlay, and the recognition query's lateral join over `fulfillments`.
