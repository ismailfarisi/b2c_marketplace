# Phase 2 — Media Pipeline & Feed Service

**Status:** Design · 2026-07-28 · implements [PLAN-VIDEO-FIRST.md](./PLAN-VIDEO-FIRST.md) §8 Phase 2
**Scope:** upload → transcode → moderate → publish, plus a geo-filtered feed. Vendor-authored content only.
**Out of scope (later phases):** ranking signals beyond recency (Phase 4), `reel_actions` and conversion (Phase 3), UGC and seller reels (Phase 6).

---

## 1. Pipeline

```
client                    S3                     worker                     serving
  │                        │                        │                          │
  ├─ POST /reels/uploads ──┤  (reel row: draft)     │                          │
  │  ← presigned PUT URL   │                        │                          │
  ├─ PUT video ───────────►│                        │                          │
  │                        ├─ S3 event → SQS ──────►│ probe (ffprobe)          │
  ├─ POST /reels/{id}      │                        │ validate                 │
  │  (caption, tags, geo)  │                        ├─ transcode ──────────────┤
  │                        │                        ├─ moderate (parallel)     │
  │                        │                        ├─ decide                  │
  │                        │                        ├─ resolve coverage geom ──┤
  │                        │                        └─ publish ───────────────►│ feed
```

### 1.1 The upload race — design for it explicitly

Two things must both happen before a reel can publish: **the bytes arrive** and **the metadata arrives**. They are independent and can complete in either order.

- The **S3 event notification is the trigger of record.** A client that dies after `PUT` must not strand the asset — the event fires regardless.
- The `POST /reels/{id}` metadata call may arrive **before** the upload finishes (fast typist, slow network) or **after**.

So: never chain these. Both write to the `reels` row and both call the same `try_advance(reel_id)` function, which checks whether all preconditions for the next state are met and enqueues the next job if so. Whichever finishes second does the work.

**Never proxy video bytes through the Go API.** Presigned `PUT` direct to S3. A 30–60s reel is 10–30MB; for anything over 100MB use multipart. Reading that into a Go handler's memory buys nothing and costs everything.

### 1.2 State machine

```
draft ──► uploaded ──► probing ──► transcoding ──► moderating ──┬─► approved ──► published
  │                                                             │
  │                                                             ├─► rejected
  │                                                             └─► review_pending ──► approved | rejected
  └─► expired (no upload within 2h)

published ──► removed        (post-publish takedown; keeps the row, drops from feed)
any state ──► failed         (with failure_code + retry count)
```

Store as a `reel_state` enum with transitions enforced in one place — a `CHECK` constraint can't express a graph, so put it in a single Go function and let every job call it. Every transition writes `reel_state_history`.

---

## 2. Transcode

### 2.1 Provider abstraction — Mux now, MediaConvert later

```go
type TranscodeProvider interface {
    Submit(ctx context.Context, sourceURL string, profile Profile) (JobRef, error)
    // Completion arrives by webhook (Mux) or EventBridge → SQS (MediaConvert).
    // Both normalize into the same ReelRenditions payload.
}
```

Ship **Mux**: it collapses transcode, storage, delivery and player analytics into one integration, and Phase 2's job is to prove the pipeline, not to optimize margin. Move to **MediaConvert + CloudFront** when delivery volume makes the per-minute difference matter. Keeping the interface from day one makes that a config change, not a rewrite.

### 2.2 Ladder — vertical 9:16, capped at 720p

| Rendition | Resolution | Target bitrate | Notes |
|---|---|---|---|
| `360p` | 360×640 | ~600 kbps | Startup rendition on cellular |
| `540p` | 540×960 | ~1.2 Mbps | Default start on wifi |
| `720p` | 720×1280 | ~2.2 Mbps | Ceiling |

**Cap at 720p.** 1080p roughly doubles egress for a difference nobody notices on a phone-sized autoplay feed — and egress is the cost driver (PLAN-VIDEO-FIRST §9). Revisit only if analytics show sustained full-screen viewing.

HLS with 2-second segments for fast startup. Also generate per reel:

- **Poster frame** at ~1s (JPEG, 720×1280)
- **Animated preview** — 2s WebP, for scrub/hover
- **BlurHash or a 32px base64 LQIP** stored inline on the row, so the feed paints instantly before any network fetch

### 2.3 Validation gate (at probe, before spending transcode money)

Reject early: duration outside 3–90s, source > 500MB, unsupported codec, aspect ratio outside 9:16 ± tolerance, zero audio + zero video streams. Rejecting at probe costs one ffprobe; rejecting after transcode costs a transcode.

---

## 3. Moderation gate

### 3.1 Layers, ordered by cost

Run cheap and early-exiting first.

| # | Layer | Service | Why this order |
|---|---|---|---|
| 1 | Poster frame image moderation | Rekognition `DetectModerationLabels` | Cents, ~300ms. Catches the blatant cases before transcode completes |
| 2 | Caption + hashtag text | Claude (see §3.3) | Runs concurrently with transcode |
| 3 | Video frame moderation | Rekognition `StartContentModeration` | The expensive one — only after 1 and 2 pass |
| 4 | Audio → transcript → text | Transcribe (`ar-AE`, `en-US`) → Claude | Catches what the visuals don't |

Layers 3 and 4 run in parallel once transcode output exists. Store every raw label in `moderation_labels` — you will need it to tune thresholds, and you cannot tune on data you didn't keep.

### 3.2 GCC thresholds — the part Western defaults get wrong

Rekognition returns confidence 0–100. A typical US configuration auto-approves anything below ~80 on most categories. **That under-blocks badly for UAE/KSA.** Alcohol, tobacco, gambling and suggestive content carry regulatory and app-store risk here that they don't carry in the US.

| Category | Auto-reject | → Human review | Auto-approve |
|---|---:|---:|---:|
| Explicit Nudity | ≥ 60 | ≥ 25 | < 25 |
| Suggestive | ≥ 80 | ≥ 40 | < 40 |
| Alcohol | ≥ 70 | **≥ 35** | < 35 |
| Gambling | ≥ 70 | **≥ 35** | < 35 |
| Tobacco | ≥ 85 | ≥ 50 | < 50 |
| Violence | ≥ 70 | ≥ 35 | < 35 |
| Hate Symbols | ≥ 50 | ≥ 20 | < 20 |

Treat these as a **starting point to tune against your own review queue**, not as settled values. The metric to watch is the human-review queue's overturn rate: if reviewers approve >90% of what lands in the queue, your review thresholds are too tight and you're burning moderator hours; if <60%, the auto-reject threshold is too loose and bad content is reaching reviewers instead of being blocked.

### 3.3 Text moderation — where an LLM genuinely beats the AWS-native option

Comprehend's toxicity detection is weak on Arabic and effectively blind to Gulf dialect, transliterated Arabic (`3arabizi` — "7abibi", "el7amdulillah"), and code-switched Arabic-English, which is how people actually write here. This is the one place in the pipeline where a language model earns its cost.

Wire shape — structured output so the verdict is schema-guaranteed:

```json
{
  "model": "claude-opus-5",
  "max_tokens": 512,
  "output_config": {
    "effort": "low",
    "format": {
      "type": "json_schema",
      "schema": {
        "type": "object",
        "properties": {
          "verdict":     { "type": "string", "enum": ["approve", "review", "reject"] },
          "categories":  { "type": "array", "items": { "type": "string" } },
          "language":    { "type": "string" },
          "reason":      { "type": "string" }
        },
        "required": ["verdict", "categories", "language", "reason"],
        "additionalProperties": false
      }
    }
  },
  "system": [
    { "type": "text", "text": "<GCC moderation policy + few-shot examples>",
      "cache_control": { "type": "ephemeral" } }
  ],
  "messages": [{ "role": "user", "content": "<caption + hashtags>" }]
}
```

Three implementation notes:

- **`effort: "low"`** — this is classification, not reasoning. Leave adaptive thinking on at low effort rather than disabling it; on Claude Opus 5 disabled thinking has known failure modes and low effort already captures most of the saving.
- **Cache the system prompt.** It's the same policy on every call. Claude Opus 5's minimum cacheable prefix is 512 tokens, so a realistic policy prompt caches; cached reads bill at ~0.1×.
- **Confirm the Go SDK field names for `output_config`** against `github.com/anthropics/anthropic-sdk-go` when implementing — the wire shape above is authoritative, the Go binding for it isn't documented in what I have on hand, so read it rather than guessing.

**On model choice:** `claude-opus-5` ($5/$25 per MTok) is the default. At moderation volume, **Haiku 4.5 ($1/$5) is the obvious cost lever** and this is a well-scoped classification task — but that's a quality/cost tradeoff that's yours to make, not mine. See §7 for the numbers on both. One catch if you go that way: Haiku 4.5's minimum cacheable prefix is **4096 tokens**, not 512 — so a short policy prompt silently won't cache on Haiku. Either write a policy prompt long enough to cross that line (a detailed GCC policy with few-shot examples gets there naturally, and improves accuracy anyway) or accept full-price input on every call.

### 3.4 Author trust tiers — the cost control

Human-reviewing every reel from a vendor who has posted 200 clean ones is waste. Tier authors and route accordingly:

| Tier | Entry condition | Routing |
|---|---|---|
| `new` | 0 published reels | Full pipeline, pre-publish human review |
| `probation` | 1–5 published, 0 strikes | Full pipeline, human review on any flag |
| `trusted` | >5 published, 0 strikes in 90d | Auto-publish on clean automated pass; 5% random sample reviewed |
| `flagged` | ≥1 upheld strike | Pre-publish human review, all content |

**Review SLA:** vendor content < 2h, and it must be staffed — a queue nobody works is the same as no moderation. In Phase 2, with vendor-only content and an operator producing much of it, one part-time reviewer covers it. That changes completely at Phase 6.

---

## 4. Feed service

### 4.1 The coverage-geometry trick

The asymmetry from PLAN-VIDEO-FIRST §3 — services filter by *service area polygon*, vehicles by *radius* — would normally mean two different query paths and two indexes. It doesn't have to.

**At publish time, resolve every reel's eligibility into a single geography column:**

| Reel kind | `coverage` is |
|---|---|
| Vendor service reel | The union of the vendor branch's `service_areas` polygons |
| Radius-scoped reel (future: vehicles) | `ST_Buffer(point, radius_m)` |
| Global/brand reel | The city or country polygon |

Then the query is one uniform predicate with one GiST index:

```sql
ST_Intersects(r.coverage, $user_point::geography)
```

Both semantics, one index, one code path. The cost is a reconciliation job: when a vendor edits a service area, re-resolve `coverage` for that vendor's published reels. That's a background job over a small set — a fine trade for keeping the hot read path simple.

### 4.2 The Phase 2 query

Phase 2 serves **chronological**. But write it in the candidate → filter → rank → diversify shape now, with `score` as a swappable expression, so Phase 4 replaces one CTE instead of rewriting the endpoint.

```sql
WITH candidate AS (
    SELECT r.id, r.author_id, r.category_id, r.published_at, r.duration_ms
    FROM   reels r
    WHERE  r.state       = 'published'
      AND  r.visibility  = 'public'
      AND  ST_Intersects(r.coverage, $1::geography)      -- $1 = user point
      AND  r.published_at > now() - interval '30 days'
      AND  ($2::uuid IS NULL OR r.category_id = $2)      -- optional category filter
      AND  NOT (r.id = ANY($3::uuid[]))                  -- $3 = recently-seen, from Redis
      AND  NOT EXISTS (
               SELECT 1 FROM user_blocks b
               WHERE b.user_id = $4
                 AND b.blocked_type = r.author_type
                 AND b.blocked_id   = r.author_id)
    ORDER BY r.published_at DESC
    LIMIT  300
),
scored AS (
    SELECT c.*,
           -- PHASE 2: recency decay only.
           -- PHASE 4: replace this expression with the ranking model's score.
           1.0 / (1.0 + EXTRACT(EPOCH FROM (now() - c.published_at)) / 86400.0) AS score
    FROM candidate c
),
diversified AS (
    SELECT s.*,
           row_number() OVER (PARTITION BY s.author_id   ORDER BY s.score DESC) AS author_rank,
           row_number() OVER (PARTITION BY s.category_id ORDER BY s.score DESC) AS cat_rank
    FROM scored s
)
SELECT id, author_id, category_id, published_at, duration_ms, score
FROM   diversified
WHERE  author_rank <= 2      -- max 2 reels per author per page
  AND  cat_rank    <= 10
ORDER BY score DESC, id
LIMIT  20;
```

Two things this deliberately does **not** do:

- **Exclusion via Redis, not a join.** `feed_impressions` grows without bound and a `NOT EXISTS` against it degrades fast. Keep a per-user seen-set in Redis with a 7-day TTL and pass the IDs as an array. The table still exists — for analytics, not for serving.
- **Diversification caps per page, not consecutive spacing.** The window functions limit *how many* reels an author gets in a page; they don't guarantee those two aren't adjacent. Do the interleave in Go after the query — it's 10 lines and it keeps the SQL simple.

**Pagination is keyset, never `OFFSET`.** Cursor on `(score, id)`; for Phase 2's chronological ordering that reduces to `(published_at, id)`.

### 4.3 Indexes

```sql
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Geo + recency in one index. btree_gist is what lets published_at ride along
-- in a GiST index; without it, the planner scans geo then sorts everything.
CREATE INDEX idx_reels_feed ON reels
    USING gist (coverage, published_at)
    WHERE state = 'published' AND visibility = 'public';

CREATE INDEX idx_reels_author_recent ON reels (author_id, published_at DESC);
CREATE INDEX idx_reel_renditions_reel ON reel_renditions (reel_id);
CREATE INDEX idx_moderation_jobs_pending ON moderation_jobs (state, created_at)
    WHERE state IN ('queued', 'running');
```

The `btree_gist` extension is the load-bearing detail — a plain GiST index on `coverage` alone forces the planner to fetch every geo match and sort them, which is fine at 5k reels and painful at 500k.

### 4.4 Where OpenSearch fits

**Not in Phase 2.** Chronological + geo + one category filter is squarely within what PostGIS does well, and adding a second datastore to the critical path before you have ranking signals is premature.

But **ship the indexer in Phase 2 anyway**: an outbox consumer that mirrors every published reel into OpenSearch. It's a day of work, it runs cold with no read traffic, and it means Phase 4 flips the read path against an index that's already warm and already correct — instead of doing a data migration under production load.

*(This refines what PLAN-VIDEO-FIRST §3 said — that doc called for OpenSearch "from day one." The better shape is: index from day one, serve from Postgres until ranking needs more.)*

---

## 5. Engagement counters and events

**Counters in Redis, truth in Postgres.** A hash per reel (`views`, `likes`, `comments`, `shares`, `saves`), incremented on write, flushed to `reel_counters` every ~10s by a River job. Feed reads take counters from Redis with a Postgres fallback. Never `UPDATE reels SET view_count = view_count + 1` on the hot path — that serializes every viewer of a popular reel behind one row lock.

**Define a "view" precisely and write it down:** ≥2 seconds watched, deduped per `(user, reel, session)`. Track completion rate as a separate metric — the two answer different questions and conflating them will mislead the Phase 4 ranking work.

**Client batches events; the server never takes one HTTP request per event.** `view_start`, `view_progress` (25/50/75/100), `like`, `comment`, `share`, `save`, `skip`, and — from Phase 3 — `action_click`. POST a batch every ~10s or on app background → ingest endpoint → Kinesis → ClickHouse.

**Start collecting in Phase 2 even though ranking is Phase 4.** You cannot train a ranking model on data you didn't collect, and the gap between "we started logging" and "we can rank" is measured in weeks of accumulated events. This is the cheapest thing in this document and the easiest to defer by accident.

---

## 6. Schema — Phase 2 subset (12 tables)

```sql
-- ── media ────────────────────────────────────────────────────────────────
CREATE TABLE media_assets (
    id              uuid PRIMARY KEY,
    owner_type      text        NOT NULL,      -- 'vendor_branch' | 'user'
    owner_id        uuid        NOT NULL,
    s3_key          text        NOT NULL,
    bytes           bigint,
    mime_type       text,
    duration_ms     int,
    width           int,
    height          int,
    probe           jsonb,                     -- raw ffprobe output
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- ── reels ────────────────────────────────────────────────────────────────
CREATE TYPE reel_state AS ENUM (
    'draft','uploaded','probing','transcoding','moderating',
    'review_pending','approved','published','rejected','removed','failed','expired'
);

CREATE TABLE reels (
    id              uuid PRIMARY KEY,
    -- Polymorphic from the first migration — see SCOPE-EXTENSIONS §5.
    author_type     text        NOT NULL,      -- 'vendor_branch' | 'user'
    author_id       uuid        NOT NULL,
    subject_type    text,                      -- 'service_offering' | 'vehicle_listing' | NULL
    subject_id      uuid,
    media_asset_id  uuid        REFERENCES media_assets(id),

    caption         text,
    caption_locale  text,                      -- 'ar' | 'en' | 'mixed'
    category_id     uuid,
    duration_ms     int,

    state           reel_state  NOT NULL DEFAULT 'draft',
    visibility      text        NOT NULL DEFAULT 'public',
    failure_code    text,

    -- Resolved eligibility geometry — see §4.1
    coverage        geography,
    coverage_kind   text,                      -- 'service_area' | 'radius' | 'global'
    origin_point    geography(Point, 4326),

    poster_key      text,
    preview_key     text,
    lqip            text,                      -- BlurHash / base64 micro-thumb

    published_at    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE reel_renditions (
    id              uuid PRIMARY KEY,
    reel_id         uuid        NOT NULL REFERENCES reels(id) ON DELETE CASCADE,
    label           text        NOT NULL,      -- '360p' | '540p' | '720p'
    width           int         NOT NULL,
    height          int         NOT NULL,
    bitrate_kbps    int         NOT NULL,
    playlist_url    text        NOT NULL,
    UNIQUE (reel_id, label)
);

CREATE TABLE reel_state_history (
    id              bigserial PRIMARY KEY,
    reel_id         uuid        NOT NULL REFERENCES reels(id) ON DELETE CASCADE,
    from_state      reel_state,
    to_state        reel_state  NOT NULL,
    actor_type      text,                      -- 'system' | 'moderator' | 'author'
    actor_id        uuid,
    reason          text,
    at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE reel_tags (
    reel_id         uuid        NOT NULL REFERENCES reels(id) ON DELETE CASCADE,
    tag             text        NOT NULL,
    PRIMARY KEY (reel_id, tag)
);

-- ── moderation ───────────────────────────────────────────────────────────
CREATE TABLE moderation_jobs (
    id              uuid PRIMARY KEY,
    reel_id         uuid        NOT NULL REFERENCES reels(id) ON DELETE CASCADE,
    layer           text        NOT NULL,      -- 'poster' | 'caption' | 'video' | 'audio'
    provider        text        NOT NULL,      -- 'rekognition' | 'transcribe' | 'claude'
    provider_job_id text,
    state           text        NOT NULL,      -- 'queued' | 'running' | 'done' | 'error'
    started_at      timestamptz,
    finished_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE moderation_labels (
    id              bigserial PRIMARY KEY,
    job_id          uuid        NOT NULL REFERENCES moderation_jobs(id) ON DELETE CASCADE,
    reel_id         uuid        NOT NULL,
    category        text        NOT NULL,
    confidence      numeric(5,2) NOT NULL,
    timestamp_ms    int,                       -- frame offset, for video labels
    raw             jsonb
);

CREATE TABLE moderation_decisions (
    id              uuid PRIMARY KEY,
    reel_id         uuid        NOT NULL REFERENCES reels(id) ON DELETE CASCADE,
    decision        text        NOT NULL,      -- 'approve' | 'reject' | 'review'
    decided_by      text        NOT NULL,      -- 'auto' | 'moderator'
    moderator_id    uuid,
    categories      text[],
    notes           text,
    at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE author_trust (
    author_type     text        NOT NULL,
    author_id       uuid        NOT NULL,
    tier            text        NOT NULL DEFAULT 'new',
    published_count int         NOT NULL DEFAULT 0,
    strike_count    int         NOT NULL DEFAULT 0,
    last_strike_at  timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (author_type, author_id)
);

CREATE TABLE reel_reports (
    id              uuid PRIMARY KEY,
    reel_id         uuid        NOT NULL REFERENCES reels(id) ON DELETE CASCADE,
    reporter_id     uuid        NOT NULL,
    reason          text        NOT NULL,
    detail          text,
    state           text        NOT NULL DEFAULT 'open',
    resolved_by     uuid,
    resolved_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (reel_id, reporter_id)
);

-- ── feed / engagement ────────────────────────────────────────────────────
CREATE TABLE reel_counters (
    reel_id         uuid PRIMARY KEY REFERENCES reels(id) ON DELETE CASCADE,
    views           bigint      NOT NULL DEFAULT 0,
    likes           bigint      NOT NULL DEFAULT 0,
    comments        bigint      NOT NULL DEFAULT 0,
    shares          bigint      NOT NULL DEFAULT 0,
    saves           bigint      NOT NULL DEFAULT 0,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_blocks (
    user_id         uuid        NOT NULL,
    blocked_type    text        NOT NULL,
    blocked_id      uuid        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, blocked_type, blocked_id)
);
```

`feed_impressions` is intentionally absent from serving — Redis holds the seen-set (§4.2). Add the table when you want impression analytics, and partition it by day from the start.

---

## 7. Cost model

Per moderation call: ~800-token cached policy prompt + ~50 fresh tokens + ~100 output.

| | Opus 5 ($5/$25) | Haiku 4.5 ($1/$5) |
|---|---:|---:|
| Per call | ~$0.0067 | ~$0.0014 (uncached) / ~$0.0006 (cached) |
| Per 1,000 calls | ~$6.70 | ~$1.35 / ~$0.63 |
| At 11k calls/day¹ | **~$2,200/mo** | **~$450/mo** / ~$210/mo |

¹ 1,000 reels/day + 10,000 comments/day. Haiku's cached column requires a ≥4096-token system prompt (§3.3).

Set against video delivery at roughly **$2–4k/month at 100k views/day** (PLAN-VIDEO-FIRST §9), text moderation on Haiku is a rounding error and on Opus is comparable to the entire CDN bill. Worth a deliberate decision rather than a default.

Rekognition video moderation is priced per minute of video analyzed — at 1,000 reels/day averaging 30s that's ~500 min/day, a modest line but one to include in the model.

**Batch API (50% off) does not fit the pre-publish path** — moderation is latency-blocking. It *does* fit a nightly re-review sweep over previously-approved content when you change thresholds, which you will do.

---

## 8. API surface (Phase 2)

| Method | Path | Notes |
|---|---|---|
| `POST` | `/v1/reels/uploads` | Returns `{reel_id, upload_url, expires_at}`. Creates the `draft` row |
| `POST` | `/v1/reels/{id}` | Caption, tags, category, geo. Idempotent; may arrive before or after upload (§1.1) |
| `GET` | `/v1/reels/{id}` | Author-visible state, including rejection reason |
| `DELETE` | `/v1/reels/{id}` | → `removed` |
| `GET` | `/v1/feed` | `?lat&lng&category&cursor&limit`. The §4.2 query |
| `POST` | `/v1/reels/{id}/report` | In-feed abuse reporting — **must ship in Phase 2**, not later |
| `POST` | `/v1/events` | Batched client events → Kinesis |
| `GET` | `/v1/moderation/queue` | Admin: pending review, oldest first |
| `POST` | `/v1/moderation/{reel_id}/decide` | Admin: approve / reject + reason |

Every mutating endpoint takes `Idempotency-Key`. All of it goes in `api/openapi.yaml` first — the spec is the source of truth and CI fails on stale generated clients (PLAN.md §3).

---

## 9. Phase 2 exit criteria

Phase 2 is done when all of these are true:

1. A vendor uploads from the web dashboard; the reel transcodes, passes moderation, and appears in the feed within **5 minutes**.
2. A reel with policy-violating content is **auto-rejected**, and the author sees why.
3. A borderline reel lands in the review queue and a human decision publishes or rejects it.
4. The feed returns **only** reels whose `coverage` contains the requesting user's address — verified against a vendor whose service area deliberately excludes the test address.
5. Events are landing in ClickHouse and a query can answer "how many views did reel X get yesterday."
6. The OpenSearch indexer is running and its document count matches `published` reels.
7. A content operator has filmed and published **≥50 reels** in the launch district.

Criterion 7 is not a technical one and it is the one most likely to be quietly dropped. Don't — a feed with 6 test reels proves the pipeline works and tells you nothing about whether the product does.

---

## 10. Implementation status

Written against this design in `services/api`. What follows is what is actually
in the repository, not what is planned.

**Built and unit-tested**

| Area | Where | Notes |
|---|---|---|
| Schema | `migrations/000012_media_feed.*` | The 12 tables from §6, plus `vendor_users`, `platform_admins` and `client_events` — all three needed by this phase and absent from §6 |
| Reel state machine | `internal/modules/reels/state.go` | The graph in one value; every write to `reels.state` goes through `CanTransition` |
| The §1.1 upload race | `internal/modules/reels/advance.go` | `Advance` is a pure function of a snapshot, so correctness does not depend on which writer ran |
| Probe validation gate | `internal/modules/media/probe.go` | Reports every problem at once, and applies the rotation tag before judging aspect ratio |
| GCC thresholds | `internal/modules/moderation/thresholds.go` | §3.2 as data, with a test asserting alcohol and gambling stay stricter than Western defaults |
| Trust tiers | `internal/modules/moderation/trust.go` | Routing can only make a verdict stricter, never looser |
| Claude text layer | `internal/modules/moderation/text.go` | Structured output, `effort: low`, cached policy prefix |
| Feed query | `internal/modules/feed/service.go` | The §4.2 candidate → scored → diversified shape, keyset cursor, Go-side author spacing |
| Counters and events | `internal/modules/engagement` | Redis hot path, Postgres truth, idempotent batch ingest |
| Search indexer | `internal/modules/search` | Runs cold behind the outbox, exactly as §4.4 argues |
| Read-path split | `cmd/feed` | Owns no tables; deletable by mounting `MountFeed` in `cmd/api` |

**Deliberately stubbed, and refused in production at boot**

- **Rekognition.** `moderation.StubVisionModerator` returns no labels, which
  `Decide` reads as approve. `app.buildModerators` returns an error when
  `APP_ENV=production` rather than letting a misconfiguration silently disable
  half the gate — that failure would be invisible until it was a screenshot.
- **The transcoder.** `media.StubProvider` synthesises three renditions whose
  playlists point at nothing. Same guard.
- **ffprobe.** Real by default; `FFPROBE_BINARY=stub` skips the binary in
  development.

**Not started**

- Transcribe → audio transcript (layer 4 of §3.1). The pipeline accepts a
  transcript and moderates it; nothing produces one yet.
- Kinesis → ClickHouse. Events land in `client_events` behind
  `engagement.Sink`, which is the interface the Kinesis implementation
  satisfies.
- River. The worker polls on a 10-second ticker. Every pipeline step is
  idempotent, which is what makes the poller adequate and the eventual swap
  uninteresting.

**Nothing here has run against a database.** The migrations have never been
applied; the feed query in particular — three CTEs, two lateral joins, a JSON
aggregate, a row-comparison keyset predicate — has never been parsed by
Postgres. Assume the first `make migrate-up` finds errors.

### Where this deviates from the design above

1. **`draft → uploaded` waits for both preconditions, not just the bytes.**
   §1.2 reads as though the S3 event alone advances the reel. Gating on both
   costs a few seconds of latency in the common case and saves a full transcode
   on every upload whose author abandoned the caption form.

2. **Text categories never pass through the threshold table.** §3.3 returns a
   verdict and category names with no confidence. There is no confidence value
   that works: 50 is simultaneously tobacco's review line and hate symbols'
   reject line, so any number chosen either under- or over-fires. They are
   carried separately and combined at the verdict level instead.

3. **Three tables §6 does not list.** `vendor_users` (there was no answer to
   "may this user post as this branch"), `platform_admins` (the moderation queue
   is the first privileged surface, and "any authenticated user can approve
   content" is not a state to pass through), and `client_events` (the
   development sink behind `engagement.Sink`).
