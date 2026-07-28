# Phase 10 — Hardening

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 10
**Scope:** the two targeted load tests, metrics and the scrape endpoint, on-call runbooks, SLOs, and an honest launch-readiness assessment.
**Out of scope:** distributed tracing, alerting rules (they belong to the monitoring stack, not the repo), backup and restore (§5 — a launch blocker, and named as one).

> Phase 10 was missing from the first draft of this plan — an oversight, not a
> decision. PLAN.md §2.3 calls for a targeted load test of capacity reservation,
> and the video-first plan adds a second contention point the services plan never
> had: the feed read path under a Ramadan-scale signup spike. **Neither survives
> being skipped.**
>
> — PLAN-VIDEO-FIRST §8

Both were run. One passed cleanly. The other found something.

---

## 1. Capacity contention — the risk PLAN.md §8 names

```
400 writers, limit 100: 100 granted, 300 refused, 0 errored in 215ms (1860/s)
10 rounds of 50 reserve/release cycles returned the bucket to empty every time
```

**Exactly 100 succeeded.** Not 99, not 101.

The assertion is not about throughput and never was. One unit of overbooking is a
customer who ordered a meal that will not be cooked, and it is the failure an
optimistic implementation produces *only* under load — which is to say, on launch
day and never in development.

What makes it hold is one line in `reserve_capacity`:

```sql
UPDATE capacity_buckets SET reserved = reserved + p_units
 WHERE id = p_bucket_id AND consumed + reserved + p_units <= capacity_limit;
```

The check is in the `WHERE` clause of the update, so it is evaluated against the
row version the update itself locks. A read-then-write in application code — the
obvious implementation — has a window between the two, and 400 concurrent
writers is exactly wide enough to find it.

### 1.1 The second test is the one that would have been skipped

`TestCapacityReleaseIsNotLossy` runs ten rounds of fill-then-release and asserts
the bucket returns to empty every time.

Overbooking is caught by the `capacity_no_overbook` CHECK and fails loudly.
*Under*-booking does not: a release path that loses a unit leaves a bucket
permanently short, the branch shows as fully booked with no orders behind it, and
nobody notices until a vendor complains that they have no deliveries — by which
point the count has drifted for weeks and there is no way to know by how much.

It is the same class of bug and the opposite symptom, and it is the one nobody
writes a test for.

### 1.2 Throughput, for the capacity plan

| Concurrent writers on one bucket | Reservations/s |
|---:|---:|
| 1 | 273 |
| 8 | 386 |
| 32 | 584 |
| 128 | 713 |

Contention on a single row does not collapse — it flattens. 713/s against one
bucket is far past any plausible launch: a bucket is one branch, one resource,
one date, one window, and 713 orders per second into a single dinner slot at a
single kitchen is not a scenario. **Capacity is not the launch risk PLAN.md §8
feared it might be.**

No threshold is asserted on these numbers. A pass/fail on throughput fails on a
laptop under a background build, gets marked flaky, and is deleted within a month.

---

## 2. The feed read path — this one found something

20,000 published reels covering a ~20 km box around Dubai Marina.

### 2.1 The index holds

```
Index Scan using reels_feed_idx on reels r
  Index Cond: ((coverage && '…'::geography) AND (published_at > now() - '30 days'))
  Buffers: shared hit=3967
```

No sequential scan. `TestFeedUsesAnIndexAtScale` asserts that and nothing else,
because "the planner chose a Seq Scan over reels" means the same thing on every
machine, and a latency threshold does not.

### 2.2 But the numbers are worse than they look

| Concurrent readers | p50 | p95 | p99 | Queries/s |
|---:|---:|---:|---:|---:|
| 1 | 112 ms | 117 ms | 117 ms | 9 |
| 8 | 132 ms | 168 ms | 177 ms | 57 |
| 32 | **317 ms** | **519 ms** | **639 ms** | 88 |

**Throughput plateaus at ~88 queries per second and p99 passes half a second at
32 concurrent readers.** On a developer laptop against a container, so the
absolute numbers mean little — but the *shape* does, and the shape is what
matters: latency triples between 8 and 32 readers while throughput rises by 50%.
That is a queue forming.

### 2.3 Why, from the plan

```
Index Scan … rows=3509 loops=1
Sort  Sort Method: top-N heapsort  Memory: 48kB
Limit … rows=200
```

The GiST index answers *which reels cover this point*, and it answers it with
**3,509 rows to produce 200**. The ordering then costs a top-N heapsort over all
of them, and the 3,967 buffer hits are almost entirely that.

This is inherent to the shape, not a missing index. A GiST index on a geography
column cannot also be ordered by `published_at` — the two orderings are
unrelated, so every qualifying reel must be materialised before the top 200 can
be found. The candidate set grows linearly with content density, which is
precisely what a successful content operator produces.

### 2.4 What this means, stated plainly

[PHASE-2 §4.4](./PHASE-2-MEDIA-AND-FEED.md) made a call:

> **OpenSearch — index from day one, serve from Postgres until ranking needs
> it.**

This load test is the evidence for *when* "until" arrives. It is not when ranking
gets more sophisticated — it is when the covering candidate set for a typical
viewer exceeds roughly two thousand reels. At that density Postgres is sorting
more than it returns on every request, and no amount of index tuning changes
that.

**A concrete trigger, so it is a decision rather than a discovery:**

```sql
-- Run weekly. When the median exceeds ~2000, the OpenSearch cutover is due.
SELECT count(*) FROM reels
 WHERE state = 'published' AND removed_at IS NULL
   AND published_at > now() - interval '30 days'
   AND ST_Intersects(coverage, <a representative viewer point>);
```

The launch district will be nowhere near it. A second city at content-operator
volume plausibly is, within months.

**What was deliberately not done:** no schema change, no index added, no query
rewritten. The finding is a scaling threshold, not a bug, and optimising against
a synthetic distribution of 20,000 uniformly-scattered reels would tune for a
shape real content will not have. The measurement and the trigger are the
deliverable.

---

## 2.5 A third thing this phase found: the rollback plan did not work

Not a load test — just running the migration chain all the way down and back up,
which nothing had ever done.

```
16/d vehicles  ERROR: cannot drop table conversations because other objects
               depend on it (2BP01)
```

`listing_leads` and `listing_offers` both carry a foreign key *back* to
`conversations` — added late in Phase 6 so a lead could resume the thread it came
from — and the down migration dropped `conversations` before them.

Three phases shipped on top of that. Nobody noticed because **no migration in
this repository had ever been executed until Phase 8**, and a down migration that
has never run is a rollback plan nobody has tested, which is the same as not
having one. The failure would have surfaced during an incident, at the moment a
rollback was the plan.

Fixed by reordering. The whole chain now goes to zero and back:

```
19/d … 1/d extensions      then      1/u … 19/u boosts
```

That round trip is worth more as a regression test than most of what is in the
unit suite, and it costs eight seconds. It is not wired into CI, because CI has
no database — which is its own gap and is listed in §5.2.

---

## 3. Observability

`internal/platform/metrics`, `/metrics` on both HTTP binaries.

### 3.1 No Prometheus client, and why

The dependency is small, well-maintained and entirely reasonable — and it is also
what makes it easy to add a hundred metrics. This system needs about a dozen
numbers on call, and every one should exist because someone would page on it. A
hand-rolled registry keeps "is this metric worth carrying" in front of whoever
adds the next one. Swapping the real client in later is an implementation change
behind these types.

**Cardinality is the whole discipline.** Every label here has a closed value set
a reader can enumerate:

- The route label is the chi **pattern**, never the path. `/v1/orders/{orderId}`
  is one series; `/v1/orders/9f3c…` is one per order.
- Status **class**, not code. 401 versus 403 does not change what on-call does,
  and recording it triples the series count.
- No vendor id, no reel id, no user id. A label whose values come from user data
  is unbounded by construction.

### 3.2 The twelve, and what each is for

| Metric | Why it exists |
|---|---|
| `http_requests_total`, `http_request_duration_seconds` | RED. Answers "is the API working" without knowing the product |
| `db_pool_acquire_wait_seconds_total` | The one that pages. Requests queueing for connections is invisible in every other metric — the database looks idle, because it is |
| `capacity_rejections_total` | A rejection is the bucket model working, not an error. A step change means a launch spike *or* a branch with capacity set wrong, and those need opposite responses |
| `feed_page_duration_seconds` | The one endpoint whose latency **is** the product |
| `feed_empty_pages_total` | The most important product signal here: a viewer at that location had nothing to watch. §4 calls content supply the existential risk; this measures it in production rather than in a plan |
| `moderation_queue_items_total` | Deep and young is a busy day. Shallow and old is a queue nobody is working — only the second is an incident |
| `ledger_postings_total` | The one error class where retrying without understanding is dangerous |
| `notifications_total` | `suppressed` is a first-class outcome, not a failure: rising suppression is a consent or template problem, rising failure is a provider problem, and different people fix them |
| `boost_deliveries_total` | `expired` rising means clients are not confirming, which silently starves campaigns (PHASE-9 §3.3) |
| `job_runs_total` | A job loop that stops is invisible everywhere else: the API stays green, dashboards stay flat, and routes go unplanned until a customer calls |

### 3.3 Two placement decisions

**`/metrics` is outside `/v1`, outside auth, outside CORS.** It is scraped by
infrastructure, has no versioning contract, and requiring a token means monitoring
stops working after a credential rotation nobody connected to it. Network policy
keeps it private; application auth would keep it broken.

**The middleware runs after `RequestID` and before `Recover`.** After, so a panic
still carries an id. Before, so a request that panics is still counted — an
endpoint that only ever 500s is invisible in a metric that skips it.

### 3.4 Quantiles are honest about being approximate

`Histogram.Quantile` returns the **upper bound of the bucket the quantile falls
in**, and returns `-1` rather than a number when it falls above the highest
bucket. A p99 that looks precise and is actually interpolated between two
boundaries 1.5 seconds apart is a figure people quote in meetings.

---

## 4. SLOs

Set against what the code does, not against round numbers.

| Service | Objective | Window | Why this number |
|---|---|---|---|
| Feed page | p95 < 250 ms | 28 days | The prefetch policy keeps two reels ahead; past ~250 ms the buffer stops keeping up on a fast scroll and the user sees black |
| Feed availability | 99.5% | 28 days | The front door. Below this the product looks abandoned |
| Order placement | p95 < 800 ms | 28 days | Includes capacity reservation and a payment authorization round trip |
| Order success | 99.9% non-5xx | 28 days | A failed order is a lost transaction and, per §3, transactions are the north star |
| Delivery OTP | 95% enqueued within 10 s of dispatch | 7 days | The driver is driving to the door. Late is failed |
| Reel publish | 95% within 5 min of upload | 7 days | PHASE-2 exit criterion #1 |
| Moderation | 95% decided within 2 h | 7 days | PHASE-2's queue SLA |
| Appeals | 95% decided within 7 days | 30 days | PHASE-8 §6; app stores treat "cannot appeal" as a policy violation |
| Payout run | 100% of eligible statements issued by Monday 06:00 | Weekly | Vendors plan cash against it |

**Deliberately no SLO on:** boost delivery rate (advertiser under-delivery is a
refund, not an outage), search latency (no search surface is live), and anything
about conversion (a business metric that no engineering action reliably moves —
an SLO on it produces gaming, not reliability).

---

## 5. Launch readiness — the honest assessment

The part of this phase most worth reading, because a launch-readiness checklist
that says "ready" is not one.

### 5.1 Blocking, and not solvable in code

| # | Blocker | Why it blocks |
|---|---|---|
| 1 | **No production payment provider.** `cmd/api` refuses to boot with `APP_ENV=production` — deliberately | No money can move |
| 2 | **No SMS provider.** `LogProvider` prints delivery codes to stdout and is gated on non-production | Cash on delivery cannot complete |
| 3 | **No backup or restore, tested or otherwise.** §9 of the runbooks says so rather than pretending | Data loss is unrecoverable, and an untested restore is not a backup |
| 4 | **No tax registration on file.** `tax_registrations` is empty. A TRN cannot be obtained for a jurisdiction nobody has chosen (open decision #2) | No legal invoice can be issued |
| 5 | **No KYC provider, and no document upload endpoint.** Manual review is the launch answer and the upload path is not built | No vendor can be paid |
| 6 | **Secrets are in `.env.example`.** `JWT_SIGNING_KEY=dev-only-…` | Self-evident |

### 5.2 Blocking, and solvable in code

| # | Gap | Size |
|---|---|---|
| 7 | Strikes are never issued — `StrikeService.Issue` is not called from `moderation.SaveDecision` (PHASE-8 §8.6) | One call, plus a policy decision about which rejections carry which severity |
| 7a | **Moderators cannot watch the reel they are judging.** `poster_key` is an object-storage key and there is no signed-URL endpoint, so the admin panel renders the key. This is the largest remaining gap on that surface — see §7.3 | Small: one endpoint |
| 8 | `kyc_documents` has no upload endpoint | Small — the Phase 2 presigned-upload machinery is the right mechanism |
| 9 | Campaign funding is not wired to a payment (PHASE-9 §6.2) | Small — a third subject on `payments.Authorize` |
| 10 | No rate limiting anywhere | Medium, and the events endpoint is public and unauthenticated by design |
| 11 | Web token store is `sessionStorage` pending a BFF route (Phase 7) | Medium |
| 12 | **CI has no database**, so migrations, the down/up round trip and the load tests never run there | Medium — and §2.5 is the argument for it |

### 5.3 Not blocking, and worth knowing

- **iOS has never been compiled.** Kotlin/Native's iOS targets need a macOS host
  (`apps/mobile-kmm/README.md` §2). `iosMain` and `iosApp/` are written against
  documented APIs and are unverified.
- **The feed read path plateaus at ~88 q/s** in this environment with 20k local
  reels (§2.2). Not a launch blocker in one district; it is the trigger for the
  OpenSearch cutover PHASE-2 §4.4 already planned for.
- **`make test` excludes the KMM tests** and now also the load tests. Both are
  named in the Makefile rather than hidden.
- **No alerting rules in the repo.** Deliberate: they belong to whatever
  monitoring stack is chosen, and a Prometheus rule file for a Prometheus nobody
  has deployed is decoration. The SLOs in §4 are what the rules should be written
  against.

### 5.4 What is genuinely ready

Worth stating, because a document that only lists gaps is as unbalanced as one
that lists none.

- **150 relations across 19 migrations apply cleanly, up and down.** First
  executed in Phase 8; every migration in this repository had been written and
  never run before that.
- **The API boots against a real Postgres**, `/readyz` reports healthy, and an
  OTP round-trips end to end.
- **Capacity holds under 4× oversubscription** with zero errors (§1).
- **The seam holds, mechanically.** `internal/app/seam_test.go` parses the import
  graph and fails the build if the feed, dispatch or driver modules reach for a
  vertical — now including the ad system.
- **The ledger balances by construction.** Nothing outside `ledger` writes
  entries, and every posting path is tested for balance.

---

## 7. The admin panel

Built after the rest of this phase, and it exists because of a question this
document should have asked and did not: **PLAN-VIDEO-FIRST §8 lists eleven
phases and an admin console is a deliverable in none of them.** PLAN.md §5 P4
mentions one in passing — *"admin console (vendor approvals, disputes,
commission config, refunds)"* — and the video-first replan dropped it silently.

The consequence was concrete rather than theoretical. Phase 2 budgeted a
moderator as headcount from launch with a two-hour review SLA; until this was
built, that person's tool was `curl`. The claim system, the locale routing and
the appeal deadlines were all in place and all unusable.

`apps/web/[locale]/admin` — an overview and eight review screens.

### 7.1 What building it found in the API

**Nine `decide` endpoints and five `queue` endpoints.** Four review surfaces
could be acted on and not found: listings, identity checks, bank accounts and
campaigns. A decide endpoint with no queue behind it is reachable only by
someone who already knows the id, which means the work never reaches a reviewer
at all.

The worst of the four was campaigns. A funded campaign sat in `pending_review`
with no way to find it — the advertiser has paid and nothing is running, which
is worse than a rejection because a rejection gets refunded.

Five endpoints added: `GET /moderation/queues`, `/moderation/listings`,
`/kyc/cases`, `/payout-accounts`, `/moderation/boosts`.

### 7.2 Three decisions on the panel itself

**The overview makes the oldest wait prominent, not the depth.** A deep and
*young* queue is a busy afternoon; a shallow and *old* one is a queue nobody is
working. Only the second is an incident, and a dashboard showing depth alone
reports them identically. The SLA travels with each row from the server's own
table rather than being a number the client picked.

**The chrome is English and `dir="ltr"`; the content is `dir="auto"`.** Every
other surface in this app is Arabic-first because customers are. This one is
used by a team whose working language is the platform's and whose job is judging
content that is often Arabic — so the frame stays stable and each caption,
comment and message renders in the direction it was written. Localising the
chrome would be a translation project nobody asked for; rendering the content
wrong would make a moderator misjudge it.

**No throughput metrics, deliberately.** A moderation team measured on
items-per-hour approves faster, which is the exact failure GCC content standards
make expensive. The measure that *is* worth building — the rate at which appeals
succeed — is named on the overview as missing, and voided strikes are retained
specifically so it can be computed.

### 7.3 What the panel still cannot do

1. **Play video.** The largest gap, and it means the reel queue is not yet
   sufficient for the decision it asks for. One signed-URL endpoint away.
2. **Show KYC documents.** Same cause — `document_ref` renders as a key. This
   one is a deliberate half-measure rather than an oversight: a short-lived
   signed link for an Emirates ID scan needs its own access log, and issuing one
   from a generic media endpoint would put the highest-value object in this
   system behind the same control as a poster image.
3. **Assign or claim.** `moderation_assignments` exists, expires stale claims
   lazily, and nothing in this UI calls it — so two moderators can still open the
   same reel.
4. **Search or filter.** Every queue is oldest-first, capped at 100. Correct at
   launch volume and wrong at the first backlog.

---

## 6. Running the load tests

```bash
make loadtest    # requires infra-up and migrate-up
```

or directly:

```bash
LOADTEST_DATABASE_URL=postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable \
  go test -tags loadtest -v ./loadtest/...
```

Two gates rather than one, deliberately. The **build tag** keeps them out of
`make test` — they need a database and take minutes. The **separate environment
variable** keeps them from running against whatever `DATABASE_URL` happens to be
set to: `capacity_test.go` writes to `capacity_buckets`, and someone who ran it
against a shared environment would have a very confusing afternoon.

The fixtures create their own vendor and branch per run and delete it after, so a
failed run leaves nothing behind and two people can run them simultaneously.

`seedFeed` runs `ANALYZE reels` after its insert. Forgetting that is the most
common way a load test produces a reassuring and meaningless result — the planner
works from statistics that predate the data, and the plan being asserted is not
the plan production would get.
