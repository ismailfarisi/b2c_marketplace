# QA findings — full-surface pass

**Run:** 2026-07-29 · Chrome via Playwright and direct HTTP, against the full
local stack (Postgres + Redis + MinIO, `cmd/api`, `cmd/feed`, `cmd/worker`,
`apps/web`), seeded with [`scripts/qa-seed.sql`](../scripts/qa-seed.sql).

The headline: **cash on delivery did not work.** Phase 8 was built specifically
to fix that, its unit tests pass, and it was broken anyway — by a Postgres
parameter-type inference rule that no test in this repository could have seen.
Two more defects in the same path would each have been visible to customers
within an hour of launch.

Nothing below was found by reading code. Every one of them needed a database, a
browser, or a worker actually running.

---

## Fixed during this pass

### 1. The delivery OTP could never be sent · **critical**

`notify.loadTemplate` bound one parameter and used it twice:

```sql
AND locale = ANY (ARRAY[$3, 'en']::locale_code[])
ORDER BY (locale::text = $3) DESC
```

Postgres infers `$3` as `locale_code` from the first use, so the second reads as
`text = locale_code` — an operator that does not exist. The statement fails, the
surrounding transaction aborts, and the *next* statement reports
`current transaction is aborted`, which points at the wrong line entirely.

Because `sendCode` runs inside the dispatch transaction, the failure took the
whole dispatch with it: `POST /driver/stops/{id}/dispatch` returned 500 and the
driver could not start a cash delivery at all.

Fixed by casting each use explicitly. The same pattern in `i18n.Resolve` was
corrected before it could bite.

**Why nothing caught it:** the notify package has 20 unit tests and not one
touches a database. `psql` with a literal works — the bug only appears through a
bound parameter.

### 2. The worker re-sent every message for ever · **critical**

`notify.dispatchOne` opened a transaction, deferred a rollback, sent the
message, updated the row — and never committed. The rollback undid the update,
so the row stayed `pending` with `attempts` at zero and the next tick sent it
again. The attempt ceiling was never reached because the counter never moved.

Observed: **one delivery code sent 100 times in ten seconds.** In production
that is a customer receiving the same SMS every ten seconds and a per-message
bill with no bound. The log line said `"sent"` — truthfully — a hundred times.

Fixed by committing. Verified after: one send, `status = sent`, `attempts = 1`.

### 3. Anonymous browsing was impossible from any browser · **critical**

`cmd/api` and `cmd/feed` each had their own CORS allow-list and they had
drifted. `cmd/feed` permitted `X-Device-Id`; `cmd/api` did not — so every
anonymous call a browser makes to the transactional process failed its
preflight: `GET /feed`, `POST /events`, and `POST /actions/{id}/click`.

The last one is the expensive part. That click is Phase 3's conversion event —
the measurement the entire video-first bet is judged on — and `actions.go`
*requires* `X-Device-Id` on it for anonymous viewers. It could not be sent from
a browser, so anonymous conversions were **unrecorded rather than
under-recorded**, and the number would have read as "reels do not convert."

Fixed by moving the list to `httpx.RequestHeaders`, shared by both binaries.
curl does not send a preflight, which is why every previous check passed.

### 4. A thin district served a one-item feed · **high**

`rank` caps the exploration bucket at one item per author. That is right for
slot *allocation* — a scarce reserve should not be owned by one vendor — and
wrong as a cap on the page.

In a launch district almost everything is low-impression, so almost everything
lands in that bucket. With one vendor in range the feed served **one reel**,
however many that vendor had published. It appears exactly when supply is
thinnest, which is when the product can least afford it.

Fixed with a `backfill` that tops a short page up in score order, still bounded
by `maxPerAuthor`. Six tests, including one asserting a feed can still be
exhausted — §10 wants that; what it must not do is claim exhaustion while
holding candidates back.

### 5. The feed never told clients which reels were advertising · **high**

`feed.Item.Sponsored` was set by Phase 9's injection and the HTTP serializer
never emitted it. Labelling paid placement is a legal requirement in both launch
markets, and no client could do it. `delivery_id` was equally dead.

Fixed. Every test in the feed package works on the domain type and stops at the
boundary, which is exactly where this lived.

### 6. Dispatch told every driver a code had been sent · **medium**

`POST /driver/stops/{id}/dispatch` returned *"a delivery code has been sent to
the recipient"* on every stop, including pickups and card-paid deliveries where
none is needed and none was sent. A driver reading that at a door asks for a
code the customer never received.

`Dispatch` now returns whether one was sent and the response says which, with a
`code_sent` boolean.

### 7. The front door was a Phase 0 placeholder · **medium**

`/[locale]` still read *"Scaffold is up. The feed lands in Phase 2."* — seven
phases after Phase 2 shipped. The feed endpoint had been serving real reels for
months and nothing on the web called it.

Replaced with a real feed page in both locales: geolocation with a stated Dubai
Marina fallback, sponsored labelling, mixed-direction captions, and an explicit
"that is everything near you" rather than an infinite spinner — a feed that
*can* be exhausted is the product promise, not a failure state.

It renders posters and captions, **not video**. That is a decision: playback
belongs to `apps/mobile-kmm`, and a web page autoplaying HLS would be a worse
version of the thing the KMM app exists for.

### 8. The fraud queue overstated the cost of a moderator's own decision · **medium**

`GET /moderation/fraud` returned `weight_if_confirmed: trust.Weight(kind) * 2` —
the kind's worth at full severity. The penalty the scorer actually applies is
scaled by the signal's severity and decayed by its age.

Observed live: the queue said *"confirming costs this seller 90 points"* and
confirming cost 40. Verified by confirming one and watching the score move
80 → 60 rather than 80 → 10.

A moderator judging whether a confirmation crosses a restriction threshold was
reading a figure more than twice the truth, consistently overstating the
consequence of their own decision.

Fixed with `trust.CostOfConfirming`, which reuses the scorer's own decay and
multipliers, plus a `score_if_confirmed` — because *"does this restrict them"* is
a question about the total, not about one signal. The panel now says
"leaving this seller at 40" and marks it when that crosses the threshold. Four
tests, one of which asserts the shown figure equals the applied one.

### 9. Six paths in the contract are not mounted · **low**

Comparing `api/openapi.yaml` against `router.go`: `/me`, `/me/addresses`,
`/vendors/{vendorId}` and `/auth/logout` are described and do not exist.
(`/healthz` and `/readyz` are outside `/v1` and are fine.)

Not fixed here — each is a small feature rather than a defect, and inventing
them to satisfy the document would be the wrong direction. Recorded because
`api/openapi.yaml` is the cross-language contract now that mobile is Kotlin, and
a contract that describes four endpoints nobody implemented is one a client
author will call.

They are the natural first slice of
[PLAN-ADMIN-CONSOLE](./PLAN-ADMIN-CONSOLE.md) Phase A, which needs `/me` and a
staff-scoped vendor read anyway.

### 10. The QA fixture itself, twice

Both were `ON CONFLICT DO NOTHING` masking a conflict on a *different* key than
intended, so an insert silently no-oped and a foreign key failed three
statements later. Worth recording because it is the same shape as a bug that
would be invisible in a real migration.

### 11. Verified end to end after fixing

Re-ran the whole chain against the fixed code:

| Path | Result |
|---|---|
| Cash delivery | Arabic OTP sent **once**, wrong code 403, right code accepted, cash held |
| Feed | Both reels served; `sponsored` on the wire; preflight passes from a browser |
| Fraud confirmation | Signal confirmed by a human → trust 80 → 60, band `ok` → `watch` |
| KYC | Rejected with a reason; rejection **without** a reason refused 422 |
| Appeal | Overturned; campaign approved and now `active` |

---

## Needs your decision

Each of these I stopped short of. The engineering is clear; the answer is not
mine to pick.

### A. Moderators still cannot watch the reel they are judging

`poster_key` is an object-storage key and there is no endpoint that turns one
into a URL a browser can load. The admin panel renders the key.

**What I would do:** add `GET /media/{key}/url` returning a 5-minute signed
CloudFront URL, staff-only, and render the poster plus an HLS player in the reel
queue.

**Why I did not:** it needs a decision about KYC documents at the same time. An
Emirates ID scan behind the same generic endpoint as a poster image is the wrong
control — it needs its own access log, and probably a separate endpoint that
records *who viewed which document when*. That is a compliance design question,
not an engineering one. **Cost: ~1 day if posters and documents share an
endpoint, ~3 with a separate audited path for documents.**

### B. `LogProvider` prints delivery codes, and there is still no SMS provider

Gated on `!IsProduction()`, so it cannot ship — which means **production cannot
send a delivery code at all**, and cash on delivery is unusable in the only
environment that matters. The interface is two methods.

**What I need:** which aggregator. Unifonic and Twilio both cover UAE and KSA;
local aggregators are cheaper per message and worse at delivery reporting. This
is a procurement decision with a price attached, and Arabic messages cost more
than twice as much per segment (`notify.SMSSegments` computes it).

### C. Which moderation rejections carry which strike severity

`StrikeService.Issue` exists, decays correctly and is tested — and nothing calls
it. A moderator removing a reel records the decision and no strike, so the
ladder is built and unclimbed.

**What I need:** the mapping. Which policy codes are `minor` (60-day),
`major` (180-day), `severe` (365-day)? Inventing it in code would bury a policy
decision inside a switch statement where nobody who owns the policy will find
it. **Cost once decided: under an hour.**

### D. Staff sessions are 30 days long, on a shared origin

`REFRESH_TOKEN_TTL=720h`, `sessionStorage`, same origin as the customer app. A
staff token approves KYC and resolves disputes with money attached.

**What I would do:** short staff sessions, step-up re-auth before money-moving
actions, and move ops to `ops.` behind an IP allowlist. All three are small.

**Why I did not:** the origin split changes deployment topology and CORS config,
and you may prefer an identity provider in front instead — which would make some
of this redundant. Worth deciding before it is built twice.

### E. No rate limiting anywhere

`POST /events` and `POST /actions/{id}/click` are public and unauthenticated by
design — rate limiting, not authentication, is what was always meant to protect
them, and it does not exist.

**What I need:** where it lives. In-process (simple, per-replica, wrong under
autoscale), Redis (correct, one more dependency on the hot path), or at the CDN
(correct and free if you are already paying for one). The third is usually
right and depends on infrastructure you have not chosen yet.

---

## Checked and working

Exercised end to end against a real database, not asserted:

- **Order → cash delivery, whole chain.** Place order (options validation
  refuses a laundry order with no garment count) → payment intent → order
  confirmed and two fulfillments scheduled → console queue shows both with item
  names → summary counts → route planning creates two routes → assignment →
  driver shift → manifest with `requires_otp` and the COD amount → dispatch →
  **Arabic OTP delivered to the customer's phone in their notification locale**
  → wrong code refused with 403 → right code accepted → cash recorded as held
  against the driver.
- **Locale routing and RTL.** `/` redirects on `Accept-Language`; `/ar` renders
  `dir="rtl"` `lang="ar"`; captions carry `dir="auto"` so an English caption in
  an Arabic page and an Arabic caption in an English page both read correctly.
- **The compliance gate.** `GET /vendors/{id}/compliance` refuses payout with
  *"no approved identity check on file"* — actionable, and correct for a case
  that is submitted rather than approved.
- **Admin queue depths**, all ten, including the empty ones, with per-queue SLAs
  from the server's own table.
- **UTF-8 end to end.** Arabic and em-dashes survive Postgres → pgx → JSON
  intact; an apparent mojibake finding was my terminal's cp1252 stdout, not the
  API. Checked the raw bytes rather than trusting the render.

---

## How to reproduce this pass

```bash
make infra-up && make migrate-up
docker compose exec -T postgres psql -U marketplace -d marketplace \
  -v ON_ERROR_STOP=1 -f - < scripts/qa-seed.sql
make run-api & make run-feed & make run-worker & npm run dev:web
```

The fixture seeds deliberately broken data — an expired trade licence, a bank
account whose holder name does not match the vendor, an open fraud signal, an
overdue appeal. A QA pass against clean data exercises only the happy path, and
the happy path is not where the bugs were.

---

# Second pass — found while building the admin console (2026-07-29)

Three defects, all found by trying to *use* the system rather than by reading
it. Two are fixed; one is recorded and deferred with its reason.

## 11. Cancelling an order never gave the delivery slot back — **fixed**

`release_capacity` only releases reservations in state `held`, which is right
for what it was written for: the sweeper reclaiming abandoned checkouts. But a
reservation moves to `committed` the moment an order is confirmed, and from
that point nothing in the system could return it.

So every cancelled order permanently burned its slot. `capacity_buckets.consumed`
never came down. A branch that took and cancelled ten orders in a morning showed
as full for the rest of the day with an empty manifest — and nobody would find
it from the inside, because the counters are self-consistent. It surfaces as a
vendor asking why the bookings stopped.

Fixed by migration `000021_release_committed_capacity`, a *separate* function
rather than a new branch in `release_capacity`. That one runs on a timer; giving
the sweeper the power to un-commit confirmed orders means a bug in it silently
frees slots that live customers are holding. Different authorities, different
functions.

Verified: two buckets at `consumed = 1` went to `0` on cancel, reservations to
`released` with `released_at` set.

## 12. Fifty conflict messages were written and thrown away — **fixed**

The codebase builds errors as `fmt.Errorf("%w: <sentence>", httpx.ErrConflict)`
in fifty places, and `WriteProblem` set the title to `Conflict` and dropped the
sentence. Every 409 the API has ever returned was bare.

The sentences are the whole value. *"nothing has been captured on this order,
so there is nothing to refund — void the authorization instead"* tells an
operator what to do next; `{"title": "Conflict"}` tells them to file a ticket.

`conflictDetail` now surfaces the message, but only when `ErrConflict` is at the
front of the chain — exactly the `%w: sentence` shape. An error that picked up
the sentinel deeper down, behind a `"load order: …"` wrap that might carry a
driver message, still falls through to the bare title rather than risk putting
internals in a response body.

## 13. Card payments are authorized and never captured — **fixed**

`payments.Capture` is written, correct, and **called by nothing**. Not the API,
not the worker, not the feed binary. Card orders take an authorization at
checkout and stop there.

Consequences, in order of how much they cost:

- The platform never takes the money for a card order.
- `ledger.PostOrderCaptured` is unreachable, so a card order never appears in
  the ledger at all. Vendor payables today come only from COD collection
  (`driver.collectCash`) and subscription billing — which is why the payable
  figures look plausible and are silently missing every card sale.
- The admin console's refund action cannot work on a card order, because there
  is no capture to reverse. That is how this was found.

Fixed at the start of Phase B, where the ledger screens make the answer
checkable rather than assumed.

`driver.Complete` now emits `order.delivered` once every stop on the order has
reached a terminal state, and a worker loop captures against it. An event rather
than a direct call, for a reason bigger than layering: capturing means calling a
payment gateway, and doing that inside the delivery transaction would hold a row
lock on the fulfillment for the length of a gateway round trip — with a driver
standing in the street waiting for their app — and a gateway timeout would roll
back a delivery that physically happened. The delivery is a fact; the capture is
a consequence, and consequences retry.

Three things fall out of that and are worth naming:

- **Already-captured is success, not an error.** The loop retries, and a retry
  landing after a capture that actually succeeded must not charge twice.
- **A cash order is not a failure.** `ErrNothingToCapture` is a distinct
  sentinel so the loop can tell a declined card (retry) from an order that never
  had a hold (normal, and roughly half of them here). Without the distinction
  the queue fills with events that will be attempted forever.
- **The capture loop runs on the slowest tick in the worker**, two minutes.
  Everything else in that process has someone waiting on it. Nothing waits on a
  capture.

Verified end to end: an authorized order was delivered, the loop captured 5100
of the 6630 hold, the quote advanced to `captured`, and the vendor payable moved
5636 → 9971 — the 5100 less 15% commission, with VAT extracted from the
commission and the journal balancing to the fils. A partial refund of 1500 then
reversed 1275/214/11 proportionally, and refunding the remainder returned the
payable to exactly 5636. Two partial refunds summed back to the original split
with no drift, which is the property the proportional reversal exists for.
