-- Phase 3: the action layer and conversion attribution.
--
-- PLAN-VIDEO-FIRST §8: "first conversion loop reel → book. Instrument
-- conversion honestly before building ranking." §2 makes the stakes explicit —
-- an unactionable reel is a cost centre, and every view is paid for whether it
-- converts or not.
--
-- The schema here is shaped by one requirement above all others: Phase 4's
-- ranking model will be trained on these numbers, and a number that flatters
-- the feed teaches the model to optimise for a fiction. So the click log is
-- separate from the resolved attribution, the resolved attribution records
-- *which model produced it*, and nothing is ever recomputed in place.


-- ── actions ──────────────────────────────────────────────────────────────────

-- A CTA on a reel. The entire vocabulary the feed has for commerce.
--
-- Note what is NOT here: no offering price, no vendor name, no vertical-specific
-- column of any kind. The triple (kind, target_type, target_id) is all the feed
-- knows, and a handler in the owning vertical resolves the rest at read time.
-- The moment a column appears here that only one vertical uses, the seam has
-- leaked.
CREATE TABLE reel_actions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reel_id     uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,

    kind        text NOT NULL CHECK (kind IN (
                    'book_service', 'subscribe_plan', 'inquire_listing',
                    'open_menu', 'follow', 'share', 'external')),
    target_type text NOT NULL CHECK (target_type IN (
                    'service_offering', 'meal_plan', 'vehicle_listing',
                    'vendor_branch', 'vendor', 'url')),
    -- NULL only for `url` targets, whose destination is target_url.
    target_id   uuid,
    target_url  text,

    -- Display order. Position 0 is the primary CTA and is the one that gets
    -- pressed; everything below it is decoration.
    position    int NOT NULL DEFAULT 0 CHECK (position >= 0),
    label       text,
    label_ar    text,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT reel_actions_target_present CHECK (
        (target_type = 'url' AND target_url IS NOT NULL AND target_id IS NULL)
        OR (target_type <> 'url' AND target_id IS NOT NULL)
    ),
    UNIQUE (reel_id, position)
);

CREATE INDEX reel_actions_reel_idx ON reel_actions (reel_id, position);
-- Answers "which reels point at this offering", which is how a vendor sees the
-- content driving one product and how the funnel rolls up by target.
CREATE INDEX reel_actions_target_idx ON reel_actions (target_type, target_id);

CREATE TRIGGER reel_actions_set_updated_at
    BEFORE UPDATE ON reel_actions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ── the click log: evidence, not claim ───────────────────────────────────────

-- Every CTA press, recorded server-side.
--
-- This table exists because the client's `source_reel_id` on an order (present
-- since migration 000006) is a *claim*. A claim is fine for product behaviour
-- and useless for deciding whether to keep paying for video: a buggy client, a
-- stale deep link, or a deliberately generous SDK will all attribute orders to
-- reels that had nothing to do with them, and the number that results is the
-- one the entire Phase 4 bet rests on.
--
-- So: the client may claim, and the server independently records what it saw.
-- attribution reconciles the two and records which it believed.
CREATE TABLE action_clicks (
    id          bigserial PRIMARY KEY,
    action_id   uuid NOT NULL REFERENCES reel_actions (id) ON DELETE CASCADE,
    reel_id     uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,

    -- Nullable: anonymous browsing is a supported mode and its clicks are
    -- exactly the ones that reveal whether the feed works without a login wall.
    user_id     uuid REFERENCES users (id) ON DELETE SET NULL,
    -- Survives login. An anonymous viewer who clicks, signs up, and orders is
    -- the single most important conversion path in the product, and keying only
    -- on user_id would lose it.
    viewer_key  text NOT NULL,
    session_id  text,

    -- Where the click happened. A click from the feed and a click from a
    -- vendor's own profile page are not the same event, and averaging them
    -- hides whether the *feed* is working.
    surface     text NOT NULL DEFAULT 'feed'
                     CHECK (surface IN ('feed', 'profile', 'search', 'share_link', 'deep_link')),
    -- How much of the reel had been watched at the moment of the click. A click
    -- at 400ms is a mis-tap; a click at 12s is intent. Ranking needs to tell
    -- them apart.
    watched_ms  int,

    clicked_at  timestamptz NOT NULL DEFAULT now()
);

-- The attribution lookup: "most recent click by this viewer, within the
-- window". Ordering the index descending makes it a single backward index scan.
CREATE INDEX action_clicks_viewer_recent_idx
    ON action_clicks (viewer_key, clicked_at DESC);
CREATE INDEX action_clicks_user_recent_idx
    ON action_clicks (user_id, clicked_at DESC) WHERE user_id IS NOT NULL;
-- The funnel rollup.
CREATE INDEX action_clicks_reel_idx ON action_clicks (reel_id, clicked_at);


-- ── resolved attribution ─────────────────────────────────────────────────────

-- One row per attributed order. Written once, never updated.
--
-- PRIMARY KEY on order_id enforces the rule that makes the numbers add up: an
-- order attributes to at most one reel. Without it, an order that touched three
-- reels contributes three conversions and the funnel sums to more revenue than
-- the business made.
CREATE TABLE order_attributions (
    order_id    uuid PRIMARY KEY REFERENCES orders (id) ON DELETE CASCADE,

    reel_id     uuid REFERENCES reels (id) ON DELETE SET NULL,
    action_id   uuid REFERENCES reel_actions (id) ON DELETE SET NULL,
    click_id    bigint REFERENCES action_clicks (id) ON DELETE SET NULL,

    -- Which rule produced this row. Stored, not inferred, because the rules
    -- will change and every historical number must remain interpretable under
    -- the rule that actually produced it.
    --
    --   last_click   server saw a click by this viewer inside the window
    --   declared     client claimed a reel and the server saw no click.
    --                Reported separately; never folded into last_click.
    --   view_through viewer saw the reel but never pressed the CTA. Counted
    --                apart from clicks — folding these together is the single
    --                easiest way to make a feed look like it works.
    --   none         organic. Browse, search, repeat order.
    model       text NOT NULL CHECK (model IN ('last_click', 'declared', 'view_through', 'none')),

    -- The window this decision was made under, in seconds. A number computed
    -- under a 24h window and a number computed under a 7d window are not
    -- comparable, and six months from now nobody will remember which was in
    -- force.
    window_seconds int NOT NULL,
    -- Seconds between the click and the order. The distribution of this is what
    -- tells you whether the window is set sensibly.
    latency_seconds int,

    -- True when the viewer reached the reel by a shared link rather than the
    -- feed. A vendor pushing their own reel to their WhatsApp customers
    -- converts at rates the feed will never touch, and letting that into the
    -- headline number makes the feed look like it works when it does not.
    off_feed    boolean NOT NULL DEFAULT false,

    -- Snapshot of order value at attribution time, so the funnel does not have
    -- to re-derive GMV from a quote that may since have been adjusted.
    gmv_minor   bigint,
    currency    char(3),

    attributed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX order_attributions_reel_idx ON order_attributions (reel_id)
    WHERE reel_id IS NOT NULL;
CREATE INDEX order_attributions_model_idx ON order_attributions (model, attributed_at);


-- ── social ───────────────────────────────────────────────────────────────────

-- `follow` is one of the seven action kinds and needs somewhere to land.
CREATE TABLE follows (
    follower_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    followee_type text NOT NULL CHECK (followee_type IN ('vendor', 'vendor_branch', 'user')),
    followee_id   uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followee_type, followee_id)
);

-- "Who follows this vendor", for the follower count and for Phase 4's
-- followed-authors feed slice.
CREATE INDEX follows_followee_idx ON follows (followee_type, followee_id);


CREATE TABLE reel_saves (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reel_id    uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, reel_id)
);

CREATE INDEX reel_saves_reel_idx ON reel_saves (reel_id);
-- The user's own saved list, newest first.
CREATE INDEX reel_saves_user_idx ON reel_saves (user_id, created_at DESC);


-- ── impressions: the denominator nobody stores ───────────────────────────────

-- Phase 2 deliberately kept the seen-set in Redis and out of the serving path.
-- That was right for serving and wrong for measurement: without a durable
-- impression count there is no denominator, and "conversion rate" silently
-- becomes orders-per-click — which is the flattering one.
--
-- Written by the feed on every page served. Day-partitioned from the start,
-- because this is the highest-volume table in the product and the alternative
-- is discovering that at 400 million rows.
CREATE TABLE feed_impressions (
    reel_id     uuid NOT NULL,
    viewer_key  text NOT NULL,
    user_id     uuid,
    surface     text NOT NULL DEFAULT 'feed',
    -- Rank within the page. Position 1 gets seen; position 18 gets scrolled
    -- past. A conversion rate that ignores position compares a reel that was
    -- shown first against one that was barely shown at all.
    position    int NOT NULL,
    served_at   timestamptz NOT NULL DEFAULT now()
) PARTITION BY RANGE (served_at);

CREATE INDEX feed_impressions_reel_idx ON feed_impressions (reel_id, served_at);

-- Creates one month's partition, idempotently.
--
-- Called by the worker a month ahead. Pre-creating matters more than it looks:
-- rows that land in the DEFAULT partition *block* the later creation of any
-- partition whose range would have covered them, so a month that slips through
-- to DEFAULT cannot be given its own partition without moving rows out first.
CREATE OR REPLACE FUNCTION create_feed_impression_partition(p_month date)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_start date := date_trunc('month', p_month)::date;
    v_end   date := (date_trunc('month', p_month) + interval '1 month')::date;
    v_name  text := 'feed_impressions_' || to_char(v_start, 'YYYYMM');
BEGIN
    IF to_regclass(v_name) IS NOT NULL THEN
        RETURN v_name;
    END IF;

    EXECUTE format(
        'CREATE TABLE %I PARTITION OF feed_impressions FOR VALUES FROM (%L) TO (%L)',
        v_name, v_start, v_end);

    RETURN v_name;
END;
$$;

-- The current and next month, so the feed can serve the moment this applies and
-- the maintenance job has a month of slack before it is load-bearing.
SELECT create_feed_impression_partition(CURRENT_DATE);
SELECT create_feed_impression_partition((CURRENT_DATE + interval '1 month')::date);

-- Backstop. Without it an insert past the last created partition fails, which
-- would take the feed down rather than degrade measurement — the wrong way
-- round for a table nothing on the serving path reads.
CREATE TABLE feed_impressions_default PARTITION OF feed_impressions DEFAULT;


-- ── funnel rollup ────────────────────────────────────────────────────────────

-- Per-reel daily funnel, every stage with its own column.
--
-- Separate columns rather than one `conversion_rate`, because the rate is
-- meaningless without saying which denominator it used. Orders ÷ clicks flatters
-- by a factor of ten against orders ÷ impressions, and a dashboard that shows
-- one number lets whoever is presenting pick the flattering one by accident.
CREATE TABLE reel_funnel_daily (
    reel_id     uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,
    day         date NOT NULL,

    impressions bigint NOT NULL DEFAULT 0,
    -- ≥2s watched, deduped per (user, reel, session) — the Phase 2 definition,
    -- applied unchanged.
    views       bigint NOT NULL DEFAULT 0,
    completions bigint NOT NULL DEFAULT 0,
    clicks      bigint NOT NULL DEFAULT 0,

    -- Split by model, never summed into one "orders" figure. A view-through
    -- order and a click-through order are different claims about the feed.
    orders_last_click   bigint NOT NULL DEFAULT 0,
    orders_declared     bigint NOT NULL DEFAULT 0,
    orders_view_through bigint NOT NULL DEFAULT 0,

    gmv_last_click_minor bigint NOT NULL DEFAULT 0,
    currency    char(3) NOT NULL DEFAULT 'AED',

    computed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (reel_id, day)
);

CREATE INDEX reel_funnel_daily_day_idx ON reel_funnel_daily (day);
