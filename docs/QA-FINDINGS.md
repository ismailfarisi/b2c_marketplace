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

## 14. Platform ledger accounts duplicated, one per posting — **fixed**

Found the first time a trial balance was rendered, which is what a trial balance
is for.

`resolveAccount` upserts with `ON CONFLICT (type, owner_type, owner_id,
currency)`, and the constraint behind it was a plain `UNIQUE`. Postgres treats
NULLs as **distinct** in a `UNIQUE`, so two rows with the same type, owner_type
and currency but a NULL `owner_id` do not conflict. Platform-level accounts —
commission revenue, VAT payable, gateway receivable — all carry a NULL owner.

So the conflict never fired for them, and every capture, refund and cash
collection minted a fresh account holding exactly one entry. Vendor and driver
accounts were fine, because they have a real owner_id and the constraint did its
job. That is exactly why it survived: the numbers anyone had looked at were the
per-vendor ones, and those were correct.

Nothing was lost and nothing was mis-stated. Every entry landed on an account of
the right type and the books balanced to the fils. But "what is our commission
revenue this month" was unanswerable without summing an unbounded set of
identically-named accounts, and that set grew by one row per order.

**The first fix attempt was wrong and the database said so.** Migration 22
originally repointed entries onto one surviving account per identity; Postgres
refused:

> `ledger_entries is append-only: UPDATE is not permitted. Post a reversing
> journal instead.`

Correct, and a better answer than the one being attempted — merging entries
would have rewritten history to make a report tidier, which is precisely what an
append-only ledger exists to prevent.

So the duplicates stay. The superseded rows are deactivated and a partial unique
index (`NULLS NOT DISTINCT`, `WHERE is_active`) makes new ones impossible; the
upsert now targets that index. The trial balance groups by *identity* rather than
by row, so the historical rows aggregate into the account the accounting always
meant. That is a presentation decision made in the read model, where it belongs,
rather than a data change made in a migration, where it would be a lie.

Verified: the trial balance now reads one row per account type, balanced at
16830/16830, with gateway receivable netting to zero across a capture and two
refunds.

## 15. No PII read has ever been audited — **fixed**

The console's §3.4 promise is that reading personal data is itself an event: the
question after a breach is *who looked at it*, not who changed it. Every detail
handler calls `RecordAccess`, and the table was empty.

`audit_log.ip` is `inet`. pgx sent the Go string as an untyped parameter and
Postgres refused the assignment — `column "ip" is of type inet but expression is
of type text`. Because `RecordAccess` logs its failures rather than returning
them (deliberately: an audit write must never fail a read), the console reported
that it was recording who read what, the warning went to a log nobody greps, and
nothing was written. Every audited read since the feature shipped was lost.

Found by looking for a row that should have been there and was not, which is the
only way this class of bug is ever found. A feature that fails silently and is
never read back cannot be discovered any other way — which is an argument for
the access-history screen existing, not just the writes.

The second cast in the same statement was wrong for the same reason and had not
bitten yet: `request_id` was cast to `uuid` against a `text` column. It worked
only because every request id happens to be a uuid today, and the first
component to set a non-uuid correlation id would have broken auditing again, in
exactly the same invisible way.

Verified: a PII read and a media read both land in `audit_log` with the caller's
IP and request id, and the failure count in the log is zero.

## 16. Reel author joins matched nothing — **fixed**

`reels.author_type` is constrained to `'vendor_branch'` or `'user'`. Three
queries in the new supply read models joined on `author_type = 'vendor'`, which
matches no row — so the reel detail page showed a blank author, coverage
reported zero published reels in a district that had two, and the supply-gap
report listed an active vendor as having never published.

Nothing errored. A LEFT JOIN that matches nothing returns nulls, and every
number it feeds is plausibly small. Caught by noticing that the vendor page said
"2 published reels" and the supply page said "0" for the same vendor on the same
screen — two views disagreeing is the cheapest bug detector there is, and it
only works if both views exist.

The grouping was wrong in the same place for a related reason: reels belong to
*branches*, so aggregating by `author_id` counts per branch, and a vendor with
two branches looks half as active as they are. Both now join through
`vendor_branches`.

---

# Third pass — building the deferred work (2026-07-29)

## 17. A context key with no value collided with the user id — **fixed**

Found while testing view-as. `GET /orders/{id}` returned 404 under a view token
and 200 under the customer's own token, for the same order and the same subject
claim.

The cause was four characters of Go:

```go
ctxKeyUserID    ctxKey = "user_id"
ctxKeyImpersonator
ctxKeyLocale ctxKey = "locale"
```

A `const` with no expression repeats the line above it. `ctxKeyImpersonator`
therefore *was* `ctxKey("user_id")` — the same key — so
`WithImpersonator(ctx, staffID)` overwrote the acting user, and every read during
a view-as session ran as the staff member rather than the customer.

That is precisely the disclosure the feature exists to prevent: a support agent
would have been shown a view built from *their own* permissions while believing
they were seeing what the customer sees. It compiles, it vets, and it is
invisible in review.

Fixed with an explicit value and a comment saying why the value cannot be
omitted. `TestImpersonatorDoesNotDisplaceTheUser` is the regression.

## 18. `retrieve` dropped every candidate on a nil exclusion list — **fixed**

`NOT (r.id = ANY($5::uuid[]))` with a nil slice: pgx sends SQL NULL, `id =
ANY(NULL)` is NULL, `NOT NULL` is NULL, and every row fails the predicate. An
empty feed, no error anywhere.

The serving path was safe because its `union` helper always returns a non-nil
slice — safe by accident. The admin feed inspector was the first other caller
and hit it immediately. Now coalesced inside the query, which fixes it for every
caller rather than for the one that noticed.

## 19. SMS segment counting classified every template as UCS-2 — **fixed**

`{` and `}` are not in the GSM-7 basic alphabet, so counting the raw template
body made any message containing a `{{placeholder}}` count as UCS-2 — reporting
a 75-character English SMS as two segments when it is one. The braces never
reach a handset; they are substituted before sending.

Counted on the rendered shape now. The approximation is stated rather than
hidden: the placeholder *name* stands in for its value, which is the right order
of magnitude, where inventing sample data per placeholder would be confidently
wrong instead of roughly right.

The corrected numbers are the argument for showing this at all: 75 English
characters is one segment, 80 Arabic characters is two, and one en-dash pasted
from a word processor takes an English message from one to two.

## 20. A swallowed error inside a transaction — **fixed**

The erasure path checked for open disputes and, on error, set the count to zero
and carried on — reasoning that a check which cannot run should not block a legal
obligation.

That is invalid inside a transaction. A failed statement aborts it, so every
statement afterwards fails with `25P02` and the reported error is three
statements downstream of the real one. The column was also wrong
(`raised_by` for `raised_by_id`), which is what the swallowed error was hiding.

A check that cannot run is a check that has not run, and an erasure is not the
place to guess.

## 21. A tab switch mid-load blanked the feed, permanently — **fixed**

`FeedPager.refresh` cleared the list and called `loadNext`, which returned
immediately because the prefetch it had just abandoned still held the `loading`
guard. No request went out, nothing threw, and the new surface rendered blank —
no spinner, no empty reason, nothing to retry. The abandoned page then landed
*in the cleared list* and overwrote the cursor, so the feed went on paging the
old surface under the new tab's label.

Not an edge case: a prefetch is in flight for most of a scroll and the tab row
sits under the thumb.

A generation counter now marks each refresh; a response that returns under a
stale one is dropped rather than written. A counter rather than cancellation
because the pager owns no scope, and the caller cancelling its own job must not
be the only thing between a stale page and the wrong tab.

Two neighbours found in the same read. `clockSkewMillis` documented itself as
"re-sampled on refresh" and never was — a device four minutes fast at launch and
correct after an NTP sync kept rendering every auction four minutes early, and
pull-to-refresh could not fix it. And `FeedUiState.error` was still `e.message`
where every other screen had moved to `Failure`: untranslatable, unbranchable,
and *nullable* — a throwable with no message set the field back to null, which
is that field's word for nothing went wrong, so a failed load fell through to
`emptyReason` and told the viewer the marketplace had nothing for them.

Both pager defects were watched failing first: `[]` for the blank feed, `0` for
the frozen skew.

## 22. The feed's clock did not follow the auction's — **fixed**

Found on a device: a plate lot in For you showing a live "1h 49m left" pill
directly above its own Place bid button, greyed out, reason "Sold".

`reel_time_signals` is a cache and nothing invalidated it. `Projector.Refresh`
had exactly one caller in the tree — the reconcile sweep — despite its own doc
describing a vertical write path as the primary one. The API process never
constructed a Projector.

Two holes. **Anti-snipe** extended `closes_at` and told nothing, so the feed
counted down to the original instant and — non-recurring countdown means the lot
is gone — dropped a reel still taking bids, during the exact two minutes the
feature exists for. **Reconcile** could not select an ended countdown: its
work-list tested `ends_at < now()` only for `recurring` rows, so the
non-recurring case waited out the 24-hour staleness clause. That is the case
where the row wants deleting, which Refresh's doc calls the direction that
matters.

The write-path fix refreshes *after* the transaction commits. Inside it, the
projector's own pool connection reads the pre-commit row and re-caches precisely
the value it was called to replace.

## 23. Two checks that could not fail — **fixed**

Both found by planting faults rather than by reading, which is the only way this
class shows up.

`interopcheck` folded Swift extensions into the Kotlin type table during the
same pass that checked call sites, so resolution depended on filename order:
`Failure.words` is declared in Feedback.swift and used in FeedScreen.swift, and
`FeedScreen` sorts first because 'S' < 'b'. A real symbol was reported missing
while the identical call in Messaging.swift passed, for no reason but its name.
Two passes now.

`parse-swift.sh` reported "no Swift toolchain — skipping" on a machine that had
one: `$USER` is unset in Git Bash and `set -u` aborted the search. A skippable
check that can reach its skip by accident is worse than no check, because the
outcome looks like success.

## 24. "Allow camera" was a button that did nothing, forever — **fixed**

Android stops showing the permission dialog after the second refusal and
`launch` then returns denied without drawing anything; iOS `requestAccess` never
prompts twice at all and returns the stored answer immediately. Both platforms
re-asked anyway, and neither mentioned Settings — the only place the decision
can still be changed. A hard dead end on the tab this product gets its supply
from.

Android pairs `shouldShowRequestPermissionRationale` with a record of having
asked, because alone it cannot tell "never asked" from "permanently denied" —
both are false. iOS reads `authorizationStatus`, which distinguishes them
exactly. Both re-read on resume, or the seller grants it in Settings, returns to
the same refusal screen, and concludes the setting did not take.

Walked on a device: first refusal still offers "Allow camera"; the second
switches to "Open settings", which lands on the app's own App info page;
granting there and returning brings up a live viewfinder with no relaunch.

## 25. Three facts rendered as one on the Vertical step — **fixed**

Still loading, the request failed, and this deployment genuinely has nothing
sellable all drew "Nothing is sellable right now", because the only question
asked was whether the list was empty. A false statement about a working product,
shown for the whole of the first fetch and every failure, to someone who has
just recorded a video.

Separately, a failed tile tap was invisible: picking a vertical fetches its
form, and when that failed the view model set an error and returned the seller
to the grid, where nothing drew it. The tile simply did not respond.

`SellUiState.error` was the last `e.message` in the app — untranslatable,
unbranchable, and nullable, so a throwable with no message rendered a failed
publish as a silent success.

## 26. A retry button in Material3 purple — **fixed**

Found by looking at the offline feed rather than at the diff. The new empty
state hand-rolled its own `Text` + `TextButton` instead of reusing the app's
`FailureBlock`, and a bare `TextButton` inherits the theme's primary colour —
which this app never sets, so it came out default violet on a black feed.

The fix is to delete the duplicate and call `FailureBlock`, which already
carries the palette, the `isRetryable` rule and the button style everything else
uses. The lesson is the ordinary one: a component was reimplemented because it
was easier to write than to look for.

## Harness note: parse-swift depended on MSYS path rewriting

`swiftc.exe` was handed POSIX paths and worked only because Git Bash rewrites
them on the way to a native binary. A shell with `MSYS_NO_PATHCONV=1` — an
ordinary thing to set, and set accidentally two commands earlier — produced
seventeen "error opening input file" lines and a failure that reads like broken
Swift rather than a broken script. Converted with `cygpath` explicitly now, and
verified under both settings.

## 27. CI never built the mobile app, and could not have — **fixed**

The workflow covered migrations, Go, TypeScript and contract drift. It did not
touch `apps/mobile-kmm` at all, which is what let four compile errors sit in
`iosMain`, and the Makefile had been saying so in a comment: "the 38
shared-logic tests do not run unless CI calls test-mobile explicitly. It
should."

Three jobs added. `mobile` on Linux runs the boundary check first (seconds, and
it catches the class the expensive jobs cannot see), then the shared tests, the
Android build, and a check that the two string tables have the same key count —
a missing translation ships as an English string in an Arabic build, which no
test notices and every user does. `ios` on a macOS runner links the Kotlin
framework and runs `swiftc -typecheck` against it with the simulator SDK, which
resolves SwiftUI, UIKit, AVFoundation *and* Shared: a real type-check of every
SwiftUI file, not the syntax-only `-parse` available off a Mac. There is no
`.xcodeproj` in the tree, so this deliberately does not `xcodebuild`.
`loadtest` runs Phase 10's capacity and feed tests on master only — they seed
20,000 reels and take minutes.

Two things would have failed that CI on its first run, both invisible on this
machine because nothing here executes them:

  - **`gradlew` was mode 100644 in git.** Every job that calls `./gradlew`
    would have died with "Permission denied" on any POSIX runner. It has been
    non-executable for the life of the repository; nothing had run it on Linux
    or macOS.
  - **`make test-mobile` had a literal `
` where a line continuation belonged**,
    so the recipe passed `
` to Gradle as an argument. `make` is not installed
    on this machine, which is exactly why it survived.

## 28. Phase 10 load tests, run rather than assumed

Numbers at 20,000 reels, on a laptop under Docker — a shape, not a capacity
verdict:

  - 400 writers against a limit of 100: exactly 100 granted, 300 refused, 0
    errored. No overbooking under the contention the lock exists for.
  - reserve/release returned the bucket to empty on all 10 rounds of 50.
  - 755 reservations/s at 128 concurrent writers on one bucket.
  - the feed read path uses `reels_feed_idx`; 69 ms execution at 20,000 rows.

Two things worth watching rather than fixing now. Feed p95 goes 175 ms at 8
concurrent readers to **524 ms at 32**, while throughput only moves 52 → 85
queries/s — the read path saturates rather than scales, and 32 readers is not a
large number. And the planner estimates `rows=1` for a scan that returns
**3,471**: it happened to choose the right index here, but a three-thousand-fold
underestimate is how a nested loop gets chosen somewhere else and a page takes
thirty seconds.

## 29. The API could not boot in production — **fixed**

`buildTranscoder` refused `stub` (playlists that point at nothing) and `ffmpeg`
(an in-memory job table that strands every in-flight reel on a worker restart),
and nothing else was implemented. Both refusals are right; the gap was the
absence of a third option. There was no value of `TRANSCODE_VENDOR` that both
existed and was permitted, so the process could not start with `APP_ENV=production`.

Now implemented: `media.MediaConvertProvider`, registered as `aws-mediaconvert`.

AWS Elemental MediaConvert rather than Mux or Cloudflare Stream, chosen by what
the deployment already has: the object store is S3, the region is me-central-1,
the credential chain is already wired through `objectstore`, and MediaConvert
reads and writes S3 directly — so a reel never travels through this process.
The alternatives mean shipping every upload to a third party and paying egress
to do it.

It is permitted in production for a structural reason rather than a preference:
it holds no job state here. The job lives in AWS under an id persisted with the
reel, and `Fetch` resolves it from that id alone, so a restart, a redeploy, or a
second worker running the reconciliation sweep all behave identically. That is
precisely what disqualifies the ffmpeg provider.

The subtle part is that the output location must be a pure function of the
input. `Fetch` rebuilds playlist URLs from the job, and it is called by the
reconciliation sweep as well as by the completion event — so a random per-job
prefix would work right up until an event was lost, and then produce URLs
pointing at nothing, on the path nobody exercises before production. There is a
round-trip test for exactly this, and it was watched failing: replacing the
derived prefix with `outputPrefix(source) + uuid` breaks all three renditions.

`TestNoTranscodeVendorIsProductionReady` was written as a test rather than a
TODO so that implementing a provider would break it and force this entry to be
rewritten. It did, and this is the rewrite.

## 30. Production config guardrails, tested and extended — **fixed**

The existing rules turned out to be better than assumed — the OTP dev echo, a
short signing key, `sslmode=disable`, and `auto` in a public endpoint are all
already refused in production, as are the stub and ffmpeg transcoders and a
missing moderation key. What none of them had was a test, which for the one
function whose entire job is refusing to start is a strange gap: a guardrail
nobody tests is one somebody deletes in a refactor, and the deletion is
invisible because the process still boots.

Two rules added.

**`S3_BUCKET` is now required in production.** `BuildMedia` returns nil when it
is unset and the API runs without the media surface, defended in a comment as
"a deployment that only serves orders is a legitimate configuration". True in
development. In production it means the entire point of a video-first
marketplace is switched off by an unset variable, announced by one warning
line, and discovered when someone asks why the feed is empty. The transcoder
and the moderator both hard-fail there; object storage, which both depend on,
was the one leg that did not.

**A placeholder signing key is refused.** Length is not entropy: the key this
repository's own CI uses — `ci-only-signing-key-at-least-32-bytes-long` — is 42
characters and passed the length rule. A guessable signing key forges any
session in the product. Word list rather than an entropy estimate, deliberately;
a false positive here is an outage, and the words that matter are the ones this
repository actually ships.

Not required, on purpose: `S3_ACCESS_KEY_ID`. An empty value falls through to
the AWS default credential chain, so demanding it would break exactly the
IAM-role deployment that is the correct way to run this.

## 31. Two stale pins found by building the thing — **fixed**

There were no Dockerfiles at all, so nothing had ever built the services outside
a developer machine. Writing one found both immediately:

  - **CI pinned Go 1.24 while `go.mod` requires 1.26.0.** Every job passed
    anyway, because `GOTOOLCHAIN` defaults to `auto` and Go silently downloaded
    1.26 before doing anything — so the pin was decorative, CI was testing on
    whatever `go.mod` asked for, and it paid a toolchain download per job for
    the privilege. The Alpine builder sets `GOTOOLCHAIN=local`, which turns the
    silent upgrade into an honest failure, which is how it surfaced.
  - **The Dockerfile's own comment claimed tzdata was "baked in below"** and it
    was not installed. Alpine ships no zoneinfo, so the runtime `COPY` failed.
    Had that copy silently succeeded, the container would have rendered every
    settlement date in UTC — the bug already fixed once in finding #14.

The images are distroless, non-root, 31–38 MB, and are verified at publish time
by being run with a deliberately invalid production config: the container must
exit non-zero and say why. That checks the binary is executable in the base
image and that the guardrails above are compiled in, in one step.

## Harness note: the shell was silently destroying Arabic

An early test of the template editor sent Arabic through a bash heredoc into
`curl` and reported a pass. The stored bytes were `3f3f3f…` — question marks.
Git Bash on this machine is cp1252, and the text was flattened before it reached
the API.

The test *looked* correct because ASCII question marks happen to count as one SMS
segment, which is what an Arabic string under 70 characters counts as. Two wrongs
producing the expected number.

Re-tested by POSTing from Python with an explicit UTF-8 body, which round-tripped
intact and produced the *real* answer: two segments, not one. Where a test crosses
this shell, encoding has to be bypassed rather than trusted.
