-- Phase 4: ranking and social.
--
-- PLAN-VIDEO-FIRST §8: "Likes, comments, follows, saves. Real ranking with
-- exploration. Interest profiles." Follows and saves landed in Phase 3; this
-- migration adds the other two, plus everything ranking needs to stop being
-- `ORDER BY published_at`.
--
-- One rule shapes every table below, and it is inherited from Phase 3: **store
-- counts, never rates.** A stored `engagement_rate` is a number whose
-- denominator has been thrown away, and six months later nobody can tell whether
-- it was per-impression or per-view. Every rate in this system is computed at
-- read time, in one Go function, from counts that say what they are.


-- ── likes ────────────────────────────────────────────────────────────────────

-- Truth for the like counter that lives in Redis.
--
-- The counter is the hot path and this table is the record. They will drift —
-- Redis takes increments the row never receives, and the flush reconciles
-- upward — and that is acceptable for a badge on a card. It is not acceptable
-- for "have I liked this", which is a per-viewer fact the client renders as a
-- filled heart, so that question is answered from here and never from a counter.
CREATE TABLE reel_likes (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reel_id    uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, reel_id)
);

-- "Who liked this reel", for the author's audience view and for the ranking
-- feature refresh.
CREATE INDEX reel_likes_reel_idx ON reel_likes (reel_id, created_at DESC);


-- ── comments ─────────────────────────────────────────────────────────────────

-- Comments are the first thing in this product a member of the public can write
-- that another member of the public reads.
--
-- Which makes them a moderation surface before they are a feature. §6 of the
-- plan is explicit that GCC standards are stricter than the defaults these
-- services ship with, and a comment is cheaper to post than a reel by three
-- orders of magnitude — so the abuse arrives here first and in volume.
--
-- Two consequences are encoded below:
--   1. `state` defaults to 'pending'. A comment is invisible until something
--      has judged it. There is no path that inserts a visible comment directly.
--   2. author_id is NOT NULL. Anonymous viewing is supported and anonymous
--      *writing* is not: an anonymous comment cannot be struck, rate-limited
--      across sessions, or held against a reputation.
CREATE TABLE reel_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reel_id    uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    author_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- One level of replies, enforced by the depth trigger below rather than by
    -- convention. Unbounded nesting is a rendering problem on a phone in
    -- portrait, and a thread nobody can read is a thread nobody moderates.
    parent_id  uuid REFERENCES reel_comments (id) ON DELETE CASCADE,
    depth      smallint NOT NULL DEFAULT 0 CHECK (depth IN (0, 1)),

    body       text NOT NULL CHECK (length(btrim(body)) BETWEEN 1 AND 600),
    -- Detected, not declared. Feeds the moderation prompt, which needs to know
    -- whether it is reading Gulf Arabic, transliterated Arabic in Latin script,
    -- or English — and gets all three inside one sentence in this market.
    language   text,

    state      text NOT NULL DEFAULT 'pending'
                    CHECK (state IN ('pending', 'published', 'rejected', 'removed')),
    -- Shown to the author when their comment is rejected. "Your comment was
    -- removed" with no reason is the single most common complaint about every
    -- platform that has comments.
    reject_reason text,

    -- Soft delete. The row survives an author deleting their own comment
    -- because the replies underneath it did not consent to disappearing, and
    -- because a deleted comment is evidence when the thread is reported.
    deleted_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The thread read: published roots newest-first, then their replies. Partial on
-- state because a moderated-out comment is never rendered and indexing it costs
-- write throughput on the abusive traffic this index exists to survive.
CREATE INDEX reel_comments_thread_idx
    ON reel_comments (reel_id, created_at DESC)
    WHERE state = 'published' AND deleted_at IS NULL;

CREATE INDEX reel_comments_replies_idx ON reel_comments (parent_id, created_at)
    WHERE parent_id IS NOT NULL;

-- The rate-limit lookup and the "show me everything this account wrote" query a
-- moderator runs the moment one comment turns out to be part of a campaign.
CREATE INDEX reel_comments_author_idx ON reel_comments (author_id, created_at DESC);

-- The moderation queue.
CREATE INDEX reel_comments_pending_idx ON reel_comments (created_at)
    WHERE state = 'pending';

CREATE TRIGGER reel_comments_set_updated_at
    BEFORE UPDATE ON reel_comments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Depth is derived, never supplied.
--
-- A CHECK constraint cannot look at the parent row, so the invariant "replies
-- do not have replies" needs a trigger. Computing depth here rather than
-- trusting the caller also means a client that posts a reply-to-a-reply gets a
-- clear error instead of quietly creating a thread the UI cannot render.
CREATE OR REPLACE FUNCTION reel_comments_set_depth()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    v_parent_depth smallint;
    v_parent_reel  uuid;
BEGIN
    IF NEW.parent_id IS NULL THEN
        NEW.depth := 0;
        RETURN NEW;
    END IF;

    SELECT depth, reel_id INTO v_parent_depth, v_parent_reel
      FROM reel_comments WHERE id = NEW.parent_id;

    IF v_parent_depth IS NULL THEN
        RAISE EXCEPTION 'parent comment % does not exist', NEW.parent_id;
    END IF;
    IF v_parent_depth >= 1 THEN
        RAISE EXCEPTION 'replies to replies are not supported';
    END IF;
    -- A reply that lands on a different reel than its parent would render under
    -- a comment nobody can see, which is how a thread becomes unmoderatable.
    IF v_parent_reel <> NEW.reel_id THEN
        RAISE EXCEPTION 'reply must belong to the same reel as its parent';
    END IF;

    NEW.depth := 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reel_comments_depth
    BEFORE INSERT ON reel_comments
    FOR EACH ROW EXECUTE FUNCTION reel_comments_set_depth();


CREATE TABLE comment_reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    comment_id  uuid NOT NULL REFERENCES reel_comments (id) ON DELETE CASCADE,
    reporter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason      text NOT NULL,
    detail      text,
    state       text NOT NULL DEFAULT 'open'
                     CHECK (state IN ('open', 'upheld', 'dismissed')),
    resolved_by uuid,
    resolved_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- Same reasoning as reel_reports: without this, one account refreshing is
    -- indistinguishable from a genuine mass report.
    UNIQUE (comment_id, reporter_id)
);

CREATE INDEX comment_reports_open_idx ON comment_reports (created_at)
    WHERE state = 'open';


-- ── ranking features ─────────────────────────────────────────────────────────

-- Per-reel signal counts over a rolling window, refreshed by the worker.
--
-- Counts only. Every ratio the ranker uses is computed in
-- internal/modules/ranking, from these columns, with Bayesian smoothing applied
-- in one place. The reason is arithmetic rather than aesthetic: a reel with 3
-- impressions and 1 click has a 33% click-through rate and no information, and
-- an unsmoothed rate column would put it at the top of the feed permanently.
--
-- The window is stored per row because it will be retuned, and a 7-day count
-- compared against a 30-day count is not a comparison.
CREATE TABLE reel_features (
    reel_id     uuid PRIMARY KEY REFERENCES reels (id) ON DELETE CASCADE,

    window_days int    NOT NULL,

    impressions bigint NOT NULL DEFAULT 0,
    -- ≥2s watched, the Phase 2 definition, unchanged.
    views       bigint NOT NULL DEFAULT 0,
    completions bigint NOT NULL DEFAULT 0,
    clicks      bigint NOT NULL DEFAULT 0,
    likes       bigint NOT NULL DEFAULT 0,
    comments    bigint NOT NULL DEFAULT 0,
    shares      bigint NOT NULL DEFAULT 0,
    saves       bigint NOT NULL DEFAULT 0,

    -- Verifiable orders only — model = 'last_click'. Declared and view-through
    -- attributions are real numbers for a dashboard and the wrong training
    -- label for a ranker: teaching the model on a claim the server could not
    -- corroborate is how it learns to optimise a fiction.
    orders      bigint NOT NULL DEFAULT 0,
    gmv_minor   bigint NOT NULL DEFAULT 0,

    -- Lifetime, not windowed. Exploration asks "has this ever been given a
    -- chance", and a rolling window would make a reel eligible for exploration
    -- again every time the window rolled past its launch.
    lifetime_impressions bigint NOT NULL DEFAULT 0,

    computed_at timestamptz NOT NULL DEFAULT now()
);

-- The ranker's join order: retrieval produces reel ids, this is the lookup.
CREATE INDEX reel_features_computed_idx ON reel_features (computed_at);


-- Per-author prior.
--
-- A new reel has no signal of its own. Without an author prior its score is
-- whatever the smoothing floor says, which means every reel from a vendor with a
-- year of good content starts level with the first upload from an account
-- created this morning. That is not neutrality — it is a subsidy paid by good
-- suppliers to spammers.
CREATE TABLE author_quality (
    author_type text NOT NULL CHECK (author_type IN ('vendor_branch', 'user')),
    author_id   uuid NOT NULL,

    published_count bigint NOT NULL DEFAULT 0,
    impressions     bigint NOT NULL DEFAULT 0,
    views           bigint NOT NULL DEFAULT 0,
    clicks          bigint NOT NULL DEFAULT 0,
    orders          bigint NOT NULL DEFAULT 0,

    -- Counted separately from strikes: a rejection at the automated gate is a
    -- content mistake, a takedown after publish means something got through.
    rejected_count  bigint NOT NULL DEFAULT 0,
    takedown_count  bigint NOT NULL DEFAULT 0,

    computed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (author_type, author_id)
);


-- ── interest profiles ────────────────────────────────────────────────────────

-- What a viewer has shown they want, per category.
--
-- Keyed on viewer_key rather than user_id, and that is the load-bearing
-- decision in this table. Anonymous browsing is a supported mode — the feed is
-- the front door and putting a login wall on it would be fatal — so a profile
-- that only exists for signed-in users personalises nothing for exactly the
-- population whose first session decides whether they ever return.
--
-- user_id is carried alongside so that a viewer who signs in can have their
-- anonymous history merged rather than discarded.
CREATE TABLE viewer_interests (
    viewer_key  text NOT NULL,
    -- No foreign key, deliberately, and matching reels.category_id which has
    -- none either: there is no categories table yet. Adding one here would be
    -- inventing a taxonomy in a ranking migration, which is where taxonomies go
    -- to be wrong. When the catalogue defines categories, both columns get the
    -- constraint in the same migration.
    category_id uuid NOT NULL,
    user_id     uuid REFERENCES users (id) ON DELETE SET NULL,

    -- Exponentially decayed, in [-1, 1]. Negative is a real value and not an
    -- error state: a viewer who has skipped nine laundry reels has told you
    -- something more useful than one who has watched none.
    affinity    double precision NOT NULL DEFAULT 0
                     CHECK (affinity >= -1 AND affinity <= 1),

    positive_signals bigint NOT NULL DEFAULT 0,
    negative_signals bigint NOT NULL DEFAULT 0,

    last_signal_at timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (viewer_key, category_id)
);

-- The retrieval join: "this viewer's categories, strongest first".
CREATE INDEX viewer_interests_lookup_idx
    ON viewer_interests (viewer_key, affinity DESC);

-- The merge-on-login lookup, and the reaper's.
CREATE INDEX viewer_interests_user_idx ON viewer_interests (user_id)
    WHERE user_id IS NOT NULL;
CREATE INDEX viewer_interests_stale_idx ON viewer_interests (updated_at);


-- Phase 2 keyed client events on user_id, which quietly means "no interest
-- profile for anonymous viewers" — and anonymous viewers are the population
-- whose first session decides whether they ever come back. action_clicks and
-- feed_impressions already carry viewer_key; this brings the event stream in
-- line so all three can be aggregated together.
ALTER TABLE client_events ADD COLUMN viewer_key text;

-- Watch depth, promoted from a jsonb property to a column.
--
-- Phase 2 kept it only inside the dedup key, on the reasoning that this table
-- is written on every batch and read by nothing. Phase 4 reads it: "a view" is
-- defined as ≥2s watched, and the ranker needs to count views, so the number
-- that decides has to be queryable. `properties->>'watched_ms'` would work and
-- would also mean the definition of a view depends on a client remembering to
-- put the right key in a free-form object.
--
-- `completed` is separate from watched_ms on purpose, and Phase 2 said why:
-- "did they watch it" and "did they finish it" answer different questions, and
-- conflating them will mislead the ranking work.
ALTER TABLE client_events ADD COLUMN watched_ms int;
ALTER TABLE client_events ADD COLUMN completed boolean NOT NULL DEFAULT false;

-- Backfill what is derivable. Signed-in events always had a key, it was just
-- spelled as a user id; anonymous history before this migration is genuinely
-- gone and is left null rather than guessed at from session_id.
UPDATE client_events
   SET viewer_key = 'u:' || user_id::text
 WHERE user_id IS NOT NULL AND viewer_key IS NULL;

-- The interest rebuild's scan: one viewer's recent events, newest first.
CREATE INDEX client_events_viewer_idx
    ON client_events (viewer_key, occurred_at DESC)
    WHERE viewer_key IS NOT NULL;


-- ── ranking weights ──────────────────────────────────────────────────────────

-- The scoring weights, in a table rather than in a constant.
--
-- Ranking is retuned on a cadence measured in days, and a weight change that
-- requires a deploy is a weight change that gets batched with an unrelated
-- feature and rolled back with it. jsonb rather than typed columns for the same
-- reason: adding a signal should not be a migration.
--
-- The compiled-in defaults in internal/modules/ranking remain authoritative
-- when this table is empty or unreadable. A ranking system whose front door
-- fails closed because a config row is missing is worse than one running on
-- last week's weights.
CREATE TABLE ranking_profiles (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL UNIQUE,
    weights    jsonb NOT NULL,
    notes      text,
    active     boolean NOT NULL DEFAULT false,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Exactly one active profile. Two would mean the feed's behaviour depended on
-- which row the query happened to read first.
CREATE UNIQUE INDEX ranking_profiles_one_active_idx ON ranking_profiles (active)
    WHERE active;

CREATE TRIGGER ranking_profiles_set_updated_at
    BEFORE UPDATE ON ranking_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The seed profile matches ranking.DefaultWeights exactly. Seeding it means the
-- first person to tune ranking edits a row that already exists rather than
-- inventing the schema of the jsonb from the Go struct.
INSERT INTO ranking_profiles (name, weights, notes, active)
VALUES (
    'phase-4-heuristic',
    '{
        "conversion": 1.0,
        "intent": 0.60,
        "engagement": 0.45,
        "author_quality": 0.25,
        "recency": 0.35,
        "affinity": 0.40,
        "followed_author": 0.30,
        "proximity": 0.15,
        "recency_half_life_hours": 72,
        "proximity_half_km": 5,
        "prior_events": 5,
        "prior_click_rate": 0.02,
        "prior_order_rate": 0.002,
        "prior_engagement_rate": 0.05,
        "takedown_penalty": 2.0,
        "reject_penalty": 0.5,
        "exploration_share": 0.15,
        "exploration_impression_floor": 500
    }'::jsonb,
    'Heuristic weights. Conversion outranks engagement by design — PLAN-VIDEO-FIRST §3 sets the north star as transactions per WAU, and watch time is the easier number to move.',
    true
);
