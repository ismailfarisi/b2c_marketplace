# Phase 4 — Ranking & Social

**Status:** Built · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §3 and §8 Phase 4
**Scope:** likes, comments, real ranking with exploration, interest profiles, ClickHouse.
**Out of scope:** meal plans (Phase 5), UGC and vehicles (Phase 6), paid reach (Phase 9), a learned model.

Follows and saves landed in Phase 3 with the action layer. This phase adds the
other two social primitives and, more importantly, stops the feed being
`ORDER BY published_at`.

---

## 1. The split: retrieval in SQL, ranking in Go

Phase 2 wrote the feed as `candidate → filter → score → diversify` in one query,
on the stated expectation that Phase 4 would replace one expression. That was
the wrong prediction, and the reason is worth recording rather than quietly
fixing.

A single scoring expression is fine in SQL. Eight terms with Bayesian smoothing,
an author prior and a per-viewer affinity join is not — **not because Postgres
cannot compute it**, but because nobody can unit-test it there. An untestable
ranker gets tuned by guessing, and the failure mode of a badly-tuned ranker is
invisible: the feed still returns twenty reels, they are just the wrong twenty.

So the endpoint is now two stages:

| Stage | Where | Question it answers |
|---|---|---|
| Retrieval | Postgres | *What may this viewer be shown?* Geometry, state, blocks. A database question, answered with indexes |
| Ranking | `internal/modules/ranking` | *What is best?* A modelling question, answered by a pure function with 30 tests |

The hard filters stay in SQL and stay hard. §3 of the plan is explicit that
deliverability and moderation state are filters and never soft signals — a
vendor who cannot deliver to you is noise regardless of how well their reel
performs.

### 1.1 The cost of the split

**Score-based keyset pagination stopped working.** Scores are computed per
request from counters that move, so a cursor holding `(score, id)` would ask the
next page to compare against a number that no longer exists. Not hypothetically:
a reel's score genuinely changes between two requests thirty seconds apart.

The cursor now carries the page index and the reels already served, packed as
raw 16-byte values. Redis holds the same exclusion information and is consulted
first; the cursor is what makes pagination *correct* when Redis is absent or has
evicted. Without it, a deployment with no Redis returns page 1 forever — a
broken feed caused by an optional dependency, which is exactly the coupling
`SeenStore`'s noop fallback exists to avoid.

The Phase 2 cursor format is not compatible and is rejected with a 422. Silently
restarting from the top would hide the client bug and give the user a feed that
mysteriously jumps back to the beginning.

---

## 2. The score

```
score = w.conversion × conversionLift          verified orders / impression
      + w.intent     × clickLift               CTA presses / impression
      + w.engagement × engagementLift          weighted like/comment/share/save
      + w.author     × authorPrior             minus takedown and reject rates
      + w.recency    × exp(−ln2 · age / halfLife)
      + w.affinity   × categoryAffinity        [−1, 1]
      + w.followed   × (1 if followed)
      + w.proximity  × 1/(1 + km/halfKm)
```

Three things in that expression are load-bearing.

### 2.1 Every rate is a *lift over baseline*

Order rates run near 0.2%; engagement rates near 5%. A linear combination of raw
rates lets engagement dominate at any weight you choose — and the person turning
`conversion` up from 1.0 to 1.5 would watch nothing happen and conclude the
signal was worthless. Dividing each smoothed rate by its own baseline puts every
term at ~1.0 for an average reel, so a weight of 1.0 genuinely means "as
important as the others".

### 2.2 The prior is measured in *events*, not impressions

This is the bug the tests caught during implementation, and it is subtle enough
to be worth stating plainly.

The obvious formulation is "smooth as if I had already seen 200 impressions at
the baseline rate". For orders at 0.2%, that is a pseudo-count of **0.4 orders**
— which a single real order overwhelms. A reel with 1 order in 3 impressions
then scores a conversion lift of 3.4 against a genuinely excellent reel's 1.5,
and sits at the top of the feed until a human notices.

Stating the prior in events fixes it and fixes it automatically for every
signal: at `prior_events = 5`, the order prior is worth 2,500 impressions and
the engagement prior 100. The scale of the smoothing follows the rarity of the
event, which is what it should have done all along.

### 2.3 Conversion outranks engagement, by construction

`TestConversionOutranksEngagement` asserts that a reel with 10,000 impressions,
900 likes and **zero orders** loses to one with fewer likes and 60 orders. §3 of
the plan sets the north star as transactions per weekly active user and names
the failure mode: watch time is the easier number to move, and a team that
optimises it ends up with an entertainment product that does not sell anything.

If that test ever fails, that has happened.

### 2.4 Weights live in a table

`ranking_profiles`, one active row, `jsonb`. Retuning ranking is a twice-a-week
activity, and a weight change that requires a deploy is one that gets batched
with an unrelated feature and rolled back with it.

Every failure path — no row, malformed JSON, failed validation, database
unreachable — falls back to the last good value and ultimately to
`ranking.DefaultWeights()`. A ranking system whose front door fails closed
because a config row is missing is worse than one running on last week's
weights. `GET /v1/ranking/profile` reports which is in force, so *"the feed
looked wrong at 14:20"* is answerable rather than debatable.

---

## 3. Exploration is a reserved slice, not a boost

15% of every page, marked `explore: true` in the response.

**Why a reserved slot rather than a score bonus.** A reel with no signal loses
every comparison by construction, so "boost new reels a bit" has exactly two
settings: too small to matter, or large enough to outrank genuinely good
content. Reserving slots is the only version with a bounded, measurable cost.

It is also a **candidate source**, not only a scoring rule. A reel that is never
retrieved cannot be ranked into a slot, so the exploration pool is queried
separately in the retrieval CTE.

Two properties of the slot placement:

- **Stable per (viewer, page).** A viewer who backgrounds the app and returns
  gets the same page rather than a reshuffled one.
- **Different across viewers.** If the exploration slot were always position 3,
  users would learn that position 3 is where the filler goes and swipe past it —
  destroying the measurement the slot exists to produce.

Never position 0. The first reel of a session decides whether there is a
session.

**Eligibility is lifetime impressions, not windowed.** A rolling window would
make a reel eligible again every time the window rolled past its launch, turning
a one-off chance into a permanent subsidy.

**The cost is real and is stated.** Those slots go to reels that are on average
worse, and the page's measured engagement drops by roughly the share reserved.
That is the price of having supply next quarter. §3: *"without it, new vendors
never get impressions and your supply side churns"* — a failure invisible from
every demand-side metric, because the reels being shown are the ones that
already earned their place.

---

## 4. Interest profiles are keyed on the viewer, not the user

`viewer_interests` is keyed on `viewer_key`, which is `u:<uuid>` for a signed-in
viewer and `d:<device>` for an anonymous one.

Anonymous browsing is a supported mode — the feed is the front door and putting
a login wall on it would be fatal. A profile that only exists for signed-in
users personalises nothing for exactly the population whose first session
decides whether they ever return.

Phase 2's `client_events` carried only `user_id`, which quietly meant the same
thing. Migration 000014 adds `viewer_key`, and adds `watched_ms` and `completed`
as columns rather than JSON properties — the definition of "a view" is ≥2s
watched, and the number that decides should not depend on a client remembering a
key name.

**Login is where an anonymous profile becomes an account's.** `LinkDevice` runs
once, on OTP verify, best-effort and after the session is minted. A viewer
browses anonymously, builds a real profile over three sessions, then signs in to
place their first order — without this call, their account starts empty and the
first personalised feed they ever see is the worst one they will ever get.

### 4.1 Negative signal counts

Affinity is in `[−1, 1]` and the negative half is not an error state. A viewer
who has skipped nine laundry reels has told you something more useful than one
who has watched none, and a model fed only positives converges on "this person
likes everything" for every person.

Signal weights encode cost to the viewer: a save (5) is an intention to come
back, a share (4) puts their reputation behind the reel, a CTA press (6) is the
only one that costs a page load, and a view (1) is nearly free. Skips are −1.5,
weighted above a view because skipping is a decision and watching is often just
not-yet-skipping.

Reversals are exactly symmetric — an unlike that decayed differently from the
like it reverses would leave a residue of a preference the viewer explicitly
withdrew.

Squashed with `tanh`, not clamped. Under a clamp every heavy user of a category
has an affinity of exactly 1.0 and the ranker loses the ability to tell "likes
food" from "orders food four times a week".

---

## 5. Comments are a moderation surface before they are a feature

This is the first place in the product where a member of the public writes
something another member of the public reads. §6 of the plan is explicit that
GCC standards are stricter than the defaults these services ship with, and that
under-blocking costs regulatory action or app-store removal rather than a bad
review.

A comment costs a viewer two seconds and costs us a moderation call. That ratio
is why the rules are what they are.

**Moderated synchronously, on the write path.** A comment that appears and then
vanishes twenty seconds later reads as censorship; one that takes three seconds
to appear reads as a slow network. `201` means published, `202` means held — a
client that renders a pending comment as live shows its author something nobody
else can see, and they find out days later that their question was never asked.

**A moderation failure holds, never publishes.** Timeout, error, missing
moderator — all yield `pending`. Comments arrive at a rate that makes "publish
while the classifier is down" equivalent to "publish everything" within minutes.
The cost is stated rather than hidden: a moderation outage means comments stop
appearing, which is visible and complainable-about. The other direction is
invisible until it is a screenshot.

**Writing requires an account; reading does not.** An anonymous comment cannot
be struck, rate-limited across sessions, or held against a reputation.

**Two levels, enforced by a trigger.** A `CHECK` cannot look at the parent row,
so depth is derived in `reel_comments_set_depth()`. Unbounded nesting is
unreadable on a phone in portrait, and a thread nobody can read is a thread
nobody moderates.

**Two rate-limit windows.** A burst limit (8 / 5 min) stops a script; a daily
limit (100 / 24h) stops a determined human working through a competitor's reels
one comment at a time — the harder and more common problem. Rejected comments
count toward both: an account whose comments are being refused is exactly the one
that should not be able to retry two hundred times.

**Soft delete.** The row survives, because the replies underneath it did not
consent to disappearing and because a deleted comment is evidence when the
thread is reported. The reel's author may delete too — a vendor cannot be told to
accept whatever appears under their storefront while waiting for a moderator.

Comments get their own moderation queue. They arrive at a rate orders of
magnitude higher than reels and are judged in seconds rather than minutes;
interleaving them would bury the queue that has a publish SLA on it.

---

## 6. Likes: two stores, two questions

- *"How many likes does this reel have"* is a badge on a card. Answered from the
  Redis counter. Being 3% low for ten seconds costs nothing.
- *"Have **I** liked this"* is a filled heart in one viewer's UI. Answered from
  `reel_likes`. Being wrong is immediately visible to the one person who knows
  the answer.

Serving the second from a counter is the bug that produces a heart which
un-fills when you scroll away and back. Viewer state for a whole feed page —
liked, saved, followed — is one query, not sixty.

---

## 7. ClickHouse in, OpenSearch still not on the read path

**ClickHouse is wired and optional.** `CLICKHOUSE_URL` unset keeps the Postgres
sink, which is correct in development and in a launch district. `CLICKHOUSE_TEE`
writes to both during a cutover — a migration that switches the old store off
first has nothing to check its first bad number against.

Publish buffers and never performs I/O; the worker flushes. Doing the insert
inline would put warehouse latency on every mobile client's event POST, and
those happen every few seconds per active viewer. On failure the batch returns
to the buffer; past 50,000 events the **oldest** are dropped, counted, and
logged at error level. A sink that quietly loses 10% of views produces a
conversion rate 11% too high, and Phase 3 spent an entire design on not letting
that number be wrong.

Written over the HTTP interface rather than a native driver. In this direction
it is one endpoint. When analytics starts *reading* in anger — Phase 9's ad
delivery is the first real consumer — a driver earns its place.

**OpenSearch is still indexed and still not served from.** Phase 4's ranking
runs against Postgres, and this document is where that refinement to §3 is
recorded for the second time: *index from day one, serve from Postgres until
retrieval genuinely needs more*. The document now carries the ranking counts and
the author prior, so the day the candidate set stops fitting a single GiST scan,
those fields have been correct for months rather than backfilled under load.
`deploy/opensearch/reels-mapping.json` uses `dynamic: strict` so a field added
to the Go struct without a mapping fails loudly at index time instead of being
silently stored as text.

---

## 8. API surface

| Method | Path | Notes |
|---|---|---|
| `PUT`/`DELETE` | `/v1/reels/{id}/like` | Idempotent; `changed` reports whether this call did anything |
| `GET` | `/v1/reels/{id}/comments` | Public. Roots newest-first, ≤3 replies inline |
| `POST` | `/v1/reels/{id}/comments` | **201 published, 202 held** |
| `DELETE` | `/v1/comments/{id}` | Author, reel author, or staff |
| `POST` | `/v1/comments/{id}/report` | |
| `GET` | `/v1/moderation/comments` | Staff. Oldest first |
| `POST` | `/v1/moderation/comments/{id}/decide` | Staff. Resolves open reports too |
| `GET` | `/v1/ranking/profile` | Staff. What is ranking the feed right now |

`GET /v1/feed` gains `explore`, `viewer.{liked,saved,followed}` per item and
`ranking_profile` on the page. `POST /v1/events` gains `completed`.

---

## 9. Exit criteria

1. Two reels with identical geo eligibility rank in the order their conversion
   and engagement justify, not in publication order.
2. A reel published this morning by an unknown vendor receives impressions —
   verifiably, via `explore: true` on served items.
3. A viewer who consistently skips one category sees less of it, and this is
   visible as a negative row in `viewer_interests`.
4. A viewer who browses anonymously and then signs in keeps their profile.
5. A comment containing content that violates GCC policy does not appear, and
   its author is told which policy.
6. A moderation outage delays comments rather than publishing them.
7. `GET /v1/ranking/profile` names the weights that produced the current feed,
   and changing that row changes ranking without a deploy.
8. Someone has compared the Phase 3 funnel before and after ranking and stated,
   in writing, whether ranking moved transactions per WAU — **not watch time**.

Criterion 8 is the point of the phase. Ranking that improves engagement and not
transactions has moved the product toward being TikTok, which is a competition
§10 says outright cannot be won.

---

## 10. Implementation status

**Built and unit-tested**

| Area | Where | Tests |
|---|---|---|
| Schema | `migrations/000014_ranking_social.*` | — |
| Scoring | `internal/modules/ranking/score.go` | 15 |
| Weights & profiles | `internal/modules/ranking/{weights,profiles}.go` | 4 |
| Exploration | `internal/modules/ranking/explore.go` | 9 |
| Feature refresh | `internal/modules/ranking/features.go` | — (SQL) |
| Interest profiles | `internal/modules/interests/` | 11 |
| Likes & viewer state | `internal/modules/social/likes.go` | — |
| Comments | `internal/modules/comments/` | 14 |
| Two-stage feed | `internal/modules/feed/{service,cursor}.go` | 14 |
| ClickHouse sink | `internal/modules/engagement/clickhouse.go` | — |

**Deviations from the plan text**

1. **Ranking did not replace one SQL expression.** §1 above. The Phase 2
   prediction was wrong and the cursor format changed as a consequence.
2. **OpenSearch is still not on the read path**, for the second phase running.
   §7. It is indexed, its mapping is explicit, and the ranking fields are
   populated — but Postgres serves the feed.
3. **No learned model.** §8 of the plan says Phase 4 needs an in-house data/ML
   capability and that heuristic ranking buys about six months. What shipped is
   the heuristic. The weights are guesses — honest ones, informed by the shape
   of the problem, but nobody has served a page with them. Open question 5 in
   the plan is unanswered and this phase does not answer it.

**Not verified.** The migrations have never been applied. Fourteen now. The
queries most worth distrusting, in order: the two-stage feed retrieval (a
materialised CTE referenced four times, four candidate sources unioned, three
left joins and a lateral aggregate), the feature refresh (nine lateral
subqueries, one of them a lifetime count over a partitioned table), and the
comment thread's reply window function.
