-- ClickHouse schema for feed engagement events.
--
-- PLAN-VIDEO-FIRST §3: view and impression events go here and never to
-- Postgres — "this is your highest-volume data by an order of magnitude". A
-- video-first front door generates one to two orders of magnitude more view
-- events than orders, and every one of them is paid for whether it converts or
-- not (§9).
--
-- Applied by hand, or by `make clickhouse-schema`. Deliberately not part of the
-- Postgres migration chain: the two stores fail independently, and a schema
-- change to the analytics warehouse must never be able to block a deploy of the
-- transactional core.

CREATE DATABASE IF NOT EXISTS analytics;

-- One row per client-reported interaction.
--
-- ReplacingMergeTree keyed on event_id, because client batches are
-- at-least-once by design: a mobile client that loses its connection mid-POST
-- resends the whole batch. Without deduplication every flaky network inflates
-- the view count, and the view count is a denominator.
--
-- Note that Replacing deduplicates *eventually*, at merge time. Any query that
-- must be exact says FINAL or aggregates with argMax; a dashboard that reads
-- the table raw is reading a slight over-count, and that is documented here so
-- nobody rediscovers it during a board meeting.
CREATE TABLE IF NOT EXISTS analytics.reel_events
(
    event_id    String,
    -- Client clock. What the viewer's device believed the time was.
    occurred_at DateTime64(3, 'UTC'),
    -- Server clock at ingest. Kept separately because the difference is the
    -- only way to spot a client delivering hours of offline events, and a view
    -- "at" 3am that arrived at 9am is not a 3am view.
    received_at DateTime64(3, 'UTC'),

    event_type  LowCardinality(String),
    reel_id     String,
    user_id     String,
    -- Survives login, and is present for anonymous viewers. Keying only on
    -- user_id would lose the single most important conversion path in the
    -- product: an anonymous viewer who watches, signs up, and orders.
    viewer_key  String,
    session_id  String,
    watched_ms  Int32,
    -- Separate from watched_ms, deliberately: "did they watch it" and "did they
    -- finish it" answer different questions, and conflating them misleads
    -- ranking.
    completed   UInt8,
    properties  String
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(occurred_at)
-- Ordered for the two queries that actually run: "everything about this reel
-- over a period" (the funnel) and "this viewer's history" (interest profiles).
ORDER BY (reel_id, occurred_at, event_id)
-- Ninety days of raw events. Rollups outlive them; the raw stream does not need
-- to, and retaining it forever is how a warehouse bill becomes the second
-- largest line item after egress.
TTL toDateTime(occurred_at) + INTERVAL 90 DAY;


-- Per-reel daily rollup, maintained incrementally.
--
-- The materialized view is what makes the funnel query cheap enough to run on a
-- dashboard refresh instead of on a schedule. Counts only — never a stored
-- rate. Phase 3's rule holds here for the same reason it holds in Postgres: a
-- rate whose denominator has been discarded gets compared against one computed
-- from a different denominator, and orders ÷ clicks flatters orders ÷
-- impressions by roughly ten times.
CREATE TABLE IF NOT EXISTS analytics.reel_events_daily
(
    day         Date,
    reel_id     String,
    event_type  LowCardinality(String),
    events      AggregateFunction(count, UInt64),
    viewers     AggregateFunction(uniq, String),
    watched_ms  AggregateFunction(sum, Int64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, reel_id, event_type);

CREATE MATERIALIZED VIEW IF NOT EXISTS analytics.reel_events_daily_mv
TO analytics.reel_events_daily
AS SELECT
    toDate(occurred_at) AS day,
    reel_id,
    event_type,
    countState()             AS events,
    uniqState(viewer_key)    AS viewers,
    sumState(toInt64(watched_ms)) AS watched_ms
FROM analytics.reel_events
GROUP BY day, reel_id, event_type;
