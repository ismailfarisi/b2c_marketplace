-- Phase 9 — Boosts.
--
-- PLAN-VIDEO-FIRST §7 lists three revenue streams and says of the third:
--
--     Boosted reels — vendors and sellers pay for reach. This is a natural,
--     high-margin unlock that Framing A never had: you own an attention surface
--     with hard geo targeting.
--
-- and then, in §9, why it matters more than "another revenue line":
--
--     Cost per view with no conversion — ~$0.11–0.17/GB egress in the region;
--     you pay for every view, converting or not.
--
-- Boosts are how the second sentence stops being a problem. Every other stream
-- earns on transactions; this one earns on the views themselves, which is the
-- cost the feed generates.
--
-- 6 tables, and one column added to `payments`.
--
-- ─────────────────────────────────────────────────────────────────────────────
-- The constraint that shapes every table below
-- ─────────────────────────────────────────────────────────────────────────────
--
-- PLAN-VIDEO-FIRST §10 states the product promise as **"everything here is
-- available to you right now"** and closes with:
--
--     Every design argument in this document should be checked against that
--     sentence. If a feature makes the feed more entertaining but less
--     transactable, it belongs in a different product.
--
-- A boost that bypassed the geo filter would sell exactly that: reach into feeds
-- where the advertiser cannot deliver. It would be the most profitable thing in
-- this schema and it would destroy the one claim the product has against TikTok.
--
-- So: **a boosted reel passes the same hard filters as an organic one.** There
-- is no column here that could switch that off, and that absence is the design.


-- ─────────────────────────────────────────────────────────────────────────────
-- 1. What reach costs
-- ─────────────────────────────────────────────────────────────────────────────

-- Rate cards, date-versioned like tax rates and for the same reason: a campaign
-- that ran in March must still be explicable in June, and a price list that only
-- knows "now" rewrites its own history the moment it changes.
--
-- CPM rather than CPC. The unit being sold is the thing that costs money to
-- deliver — a view is egress whether it converts or not — and pricing the
-- clicks while paying for the bytes is how an ad product loses money on its
-- best-performing campaigns.
CREATE TABLE boost_rate_cards (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Where the impression appears. Only 'feed' exists today; naming it means
    -- a future search or listing placement is a row rather than a migration.
    placement   text NOT NULL DEFAULT 'feed'
                     CHECK (placement IN ('feed', 'search', 'listing_detail')),
    country     char(2) NOT NULL,
    -- Reserved for city-level pricing. NULL means country-wide, which is the
    -- launch answer: one district does not need a price curve.
    region      text,

    floor_cpm_minor int NOT NULL CHECK (floor_cpm_minor > 0),
    currency    char(3) NOT NULL DEFAULT 'AED',
    valid       daterange NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT boost_rate_cards_no_overlap EXCLUDE USING gist (
        placement WITH =,
        country WITH =,
        coalesce(region, '') WITH =,
        valid WITH &&
    )
);

INSERT INTO boost_rate_cards (placement, country, floor_cpm_minor, valid) VALUES
    ('feed', 'AE', 1200, daterange(CURRENT_DATE, NULL)),
    ('feed', 'SA', 1000, daterange(CURRENT_DATE, NULL));


-- ─────────────────────────────────────────────────────────────────────────────
-- 2. Campaigns
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE boost_campaigns (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_no text NOT NULL UNIQUE DEFAULT generate_reference('BST'),

    -- Who is buying. A vendor buys through their console; a private seller
    -- buys through the listing screen. Same table, because the auction does not
    -- care and giving them separate tables would mean two of everything below.
    advertiser_type text NOT NULL CHECK (advertiser_type IN ('vendor', 'user')),
    advertiser_id   uuid NOT NULL,

    -- What is boosted. A reel, always — not a listing, not an offering.
    --
    -- That restriction is worth defending rather than relaxing. The unit of
    -- attention in this product is a reel; a "boosted listing" with no video
    -- would be a text row wedged into a video feed, which is the single most
    -- reliable way to make people stop scrolling for the wrong reason. If a
    -- seller wants reach, they film.
    reel_id     uuid NOT NULL REFERENCES reels (id) ON DELETE CASCADE,

    objective   text NOT NULL DEFAULT 'reach'
                     CHECK (objective IN ('reach', 'conversion')),

    -- The bid, per thousand impressions. Checked against the rate card's floor
    -- at creation; stored here because the floor may move and this campaign's
    -- price did not.
    bid_cpm_minor int NOT NULL CHECK (bid_cpm_minor > 0),
    currency    char(3) NOT NULL DEFAULT 'AED',

    -- Money.
    --
    -- Prepaid, and there is no credit line anywhere in this schema. An
    -- advertiser cannot spend money they have not paid, which removes the
    -- entire category of "campaign overran its budget over a weekend" — the
    -- most common way an ad product turns into a collections problem.
    funded_minor  bigint NOT NULL DEFAULT 0 CHECK (funded_minor >= 0),
    spent_minor   bigint NOT NULL DEFAULT 0 CHECK (spent_minor >= 0),
    -- Per-day ceiling. Pacing spreads it across the day rather than spending it
    -- by 09:00 — see boost_daily_spend.
    daily_cap_minor bigint NOT NULL CHECK (daily_cap_minor > 0),

    starts_on   date NOT NULL,
    ends_on     date,

    status      text NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft', 'pending_review', 'active', 'paused',
                                        'exhausted', 'completed', 'rejected')),
    -- Required on rejection, same rule as every other reviewed object here.
    review_note text,
    reviewed_by uuid REFERENCES users (id),

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT boost_campaigns_cannot_outspend_funding
        CHECK (spent_minor <= funded_minor),
    CONSTRAINT boost_campaigns_daily_cap_within_funding
        CHECK (daily_cap_minor <= funded_minor OR status = 'draft'),
    CONSTRAINT boost_campaigns_dates CHECK (ends_on IS NULL OR ends_on >= starts_on),
    CONSTRAINT boost_campaigns_rejection_has_note
        CHECK (status <> 'rejected' OR review_note IS NOT NULL)
);

CREATE INDEX boost_campaigns_advertiser_idx
    ON boost_campaigns (advertiser_type, advertiser_id, created_at DESC);
-- The auction's driving index: live campaigns with money left.
CREATE INDEX boost_campaigns_live_idx ON boost_campaigns (starts_on, ends_on)
    WHERE status = 'active';
CREATE INDEX boost_campaigns_reel_idx ON boost_campaigns (reel_id);

CREATE TRIGGER boost_campaigns_set_updated_at BEFORE UPDATE ON boost_campaigns
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Targeting.
--
-- One row per campaign, not one per dimension. Every campaign has all of these
-- and most leave them null; a key-value table would turn "does this campaign
-- match this viewer" into a self-join per candidate on the hottest read path in
-- the product.
--
-- What is *not* here is as important as what is. There is no age, no gender, no
-- inferred income and no device-price tier. Partly because PDPL makes inferred
-- demographic targeting a much harder story than it is in the US or EU, and
-- partly because the product's whole differentiator is that a reel is
-- deliverable to you — targeting on who you are rather than where you are
-- would be selling a different thing.
CREATE TABLE boost_targeting (
    campaign_id uuid PRIMARY KEY REFERENCES boost_campaigns (id) ON DELETE CASCADE,

    -- Geography, as an intersection with the advertiser's real reach rather
    -- than as a replacement for it. The campaign can *narrow* where its reel
    -- appears; it can never widen it past where the vendor actually delivers.
    -- The reel's own coverage still applies, in the same query, always.
    centre      geography(Point, 4326),
    radius_m    int CHECK (radius_m IS NULL OR radius_m BETWEEN 500 AND 100000),
    emirates    text[],

    -- Categories the viewer has affinity for. Narrowing only.
    category_ids uuid[],
    -- Locales. A campaign with Arabic creative should not be shown to someone
    -- browsing in English.
    locales     text[],

    -- Days and hours, in the market's local time. Restaurants buy iftar; that
    -- is a two-hour window and buying the whole day to reach it is how a
    -- campaign burns its budget on breakfast.
    hours_of_day int[] CHECK (hours_of_day IS NULL OR
                              (array_length(hours_of_day, 1) BETWEEN 1 AND 24)),
    days_of_week int[] CHECK (days_of_week IS NULL OR
                              (array_length(days_of_week, 1) BETWEEN 1 AND 7)),

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT boost_targeting_radius_needs_centre
        CHECK ((centre IS NULL) = (radius_m IS NULL))
);

CREATE INDEX boost_targeting_centre_idx ON boost_targeting USING gist (centre)
    WHERE centre IS NOT NULL;

CREATE TRIGGER boost_targeting_set_updated_at BEFORE UPDATE ON boost_targeting
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ─────────────────────────────────────────────────────────────────────────────
-- 3. Pacing
-- ─────────────────────────────────────────────────────────────────────────────

-- One row per campaign per day.
--
-- Exists so pacing is a read rather than an aggregate. The auction runs on every
-- feed request; asking "how much has this campaign spent today" by summing a
-- partitioned delivery table on each one would make the ad system the slowest
-- thing on the feed's hot path.
--
-- `reserved_minor` is the difference between this and a simple counter.
-- Impressions are charged when they are *confirmed*, seconds after they are
-- served, and a campaign one impression from its cap can otherwise be served
-- five hundred times in that gap. Serving reserves; confirmation converts the
-- reservation into spend; unconfirmed reservations expire.
-- Amounts here are in **micro-minor units** — millionths of a fils — and that
-- is not over-engineering, it is the only way the arithmetic works.
--
-- A 12 AED CPM is 1200 minor per thousand impressions, which is 1.2 minor per
-- impression. Rounded to an integer fils that is 1, and the platform has
-- silently discounted every campaign by 17%. Rounded up it is 2, and every
-- advertiser is overcharged by 67%. Neither is a rounding error; both are a
-- pricing bug that only shows up in a quarterly reconciliation.
--
-- So impressions accumulate in micros and are converted to fils exactly once,
-- when a charge is posted to the ledger. `boost_charges.amount_minor` is the
-- only place a division happens.
CREATE TABLE boost_daily_spend (
    campaign_id uuid NOT NULL REFERENCES boost_campaigns (id) ON DELETE CASCADE,
    day         date NOT NULL,

    served      bigint NOT NULL DEFAULT 0,
    charged     bigint NOT NULL DEFAULT 0,
    spent_micros bigint NOT NULL DEFAULT 0 CHECK (spent_micros >= 0),
    reserved_micros bigint NOT NULL DEFAULT 0 CHECK (reserved_micros >= 0),

    -- What the campaign is allowed today. Copied from the campaign at first
    -- serve so that raising a daily cap tomorrow does not retroactively change
    -- what yesterday was permitted to spend.
    cap_micros  bigint NOT NULL,

    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (campaign_id, day),

    CONSTRAINT boost_daily_spend_within_cap
        CHECK (spent_micros + reserved_micros <= cap_micros)
);

CREATE INDEX boost_daily_spend_day_idx ON boost_daily_spend (day);


-- ─────────────────────────────────────────────────────────────────────────────
-- 4. Delivery
-- ─────────────────────────────────────────────────────────────────────────────

-- One row per sponsored impression.
--
-- Partitioned by day from the start. It is the highest-volume table any phase
-- has added to Postgres apart from the location pings, and unlike `reel_views`
-- it cannot live in ClickHouse: it is the evidence behind an invoice, so it has
-- to be in the database that has transactions.
--
-- The state machine is the whole point of the table:
--
--   served    -> the auction picked it and the feed returned it
--   confirmed -> the client reported the view threshold was met; charged
--   expired   -> served, never confirmed, reservation released
--
-- Charging on `served` would be simpler and would be charging for impressions
-- nobody saw — a page prefetched and abandoned bills the advertiser for twenty
-- views that never rendered. Charging on `confirmed` costs this table its extra
-- state and is the difference between an ad product advertisers trust and one
-- they audit.
CREATE TABLE boost_deliveries (
    id          uuid NOT NULL DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL,
    reel_id     uuid NOT NULL,

    -- The same viewer key the feed, the seen-set and the click log use, so a
    -- sponsored impression joins to the rest of the funnel without a second
    -- identity scheme.
    viewer_key  text NOT NULL,
    user_id     uuid,
    -- Slot within the page. A sponsored reel at position 2 and one at position
    -- 19 are not the same product, and an eCPM that ignores position compares
    -- them as though they were.
    position    int NOT NULL,

    state       text NOT NULL DEFAULT 'served'
                     CHECK (state IN ('served', 'confirmed', 'expired')),
    -- What this impression costs if confirmed, in micro-minor units:
    -- `bid_cpm_minor × 1000`. Carried on the row rather than read from the
    -- campaign so a mid-campaign bid change cannot reprice impressions that
    -- were already served at the old price.
    price_micros bigint NOT NULL CHECK (price_micros >= 0),
    currency    char(3) NOT NULL DEFAULT 'AED',

    served_at   timestamptz NOT NULL DEFAULT now(),
    confirmed_at timestamptz,
    -- Set when the charge is rolled into a ledger journal, so the roll-up is
    -- resumable and cannot double-post.
    charge_id   uuid,

    PRIMARY KEY (id, served_at)
) PARTITION BY RANGE (served_at);

-- One impression per campaign per viewer per page position.
--
-- Guards the retry rather than the fraud: a client that re-requests a page it
-- already has must not be billed twice for it. Genuine repeat exposure across
-- sessions has a different served_at day and is a different row, which is
-- correct — that is a second impression.
CREATE UNIQUE INDEX boost_deliveries_dedupe_idx
    ON boost_deliveries (campaign_id, viewer_key, position, served_at);
CREATE INDEX boost_deliveries_campaign_idx ON boost_deliveries (campaign_id, served_at);
CREATE INDEX boost_deliveries_unbilled_idx ON boost_deliveries (served_at)
    WHERE state = 'confirmed' AND charge_id IS NULL;
CREATE INDEX boost_deliveries_pending_idx ON boost_deliveries (served_at)
    WHERE state = 'served';

CREATE OR REPLACE FUNCTION create_boost_delivery_partition(p_day date)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_start date := p_day;
    v_end   date := p_day + 1;
    v_name  text := 'boost_deliveries_' || to_char(v_start, 'YYYYMMDD');
BEGIN
    IF to_regclass(v_name) IS NOT NULL THEN
        RETURN v_name;
    END IF;
    EXECUTE format(
        'CREATE TABLE %I PARTITION OF boost_deliveries FOR VALUES FROM (%L) TO (%L)',
        v_name, v_start, v_end);
    RETURN v_name;
END;
$$;

CREATE TABLE boost_deliveries_default PARTITION OF boost_deliveries DEFAULT;


-- ─────────────────────────────────────────────────────────────────────────────
-- 5. Billing
-- ─────────────────────────────────────────────────────────────────────────────

-- Charges roll confirmed impressions into ledger journals.
--
-- Hourly rather than per impression, because posting a double-entry journal for
-- 1.2 fils would produce a ledger nobody can read and a table that outgrows
-- `ledger_entries` by two orders of magnitude. The delivery rows are the
-- evidence; this is the accounting.
CREATE TABLE boost_charges (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL REFERENCES boost_campaigns (id),
    period_start timestamptz NOT NULL,
    period_end   timestamptz NOT NULL,

    impressions bigint NOT NULL CHECK (impressions > 0),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency    char(3) NOT NULL DEFAULT 'AED',

    journal_id  uuid REFERENCES ledger_journals (id),
    created_at  timestamptz NOT NULL DEFAULT now(),

    UNIQUE (campaign_id, period_start),
    CONSTRAINT boost_charges_period CHECK (period_end > period_start)
);

CREATE INDEX boost_charges_unposted_idx ON boost_charges (created_at)
    WHERE journal_id IS NULL;

ALTER TABLE boost_deliveries
    ADD CONSTRAINT boost_deliveries_charge_fk
    FOREIGN KEY (charge_id) REFERENCES boost_charges (id);


-- ─────────────────────────────────────────────────────────────────────────────
-- 6. Funding a campaign
-- ─────────────────────────────────────────────────────────────────────────────

-- `payments` gains a third subject.
--
-- The same shape Phase 6 gave it when listing fees arrived: an exactly-one
-- check across the subjects rather than a nullable `subject_type`/`subject_id`
-- pair. Widening it again here rather than inventing a `campaign_payments`
-- table, because a top-up is a payment in every respect — it takes a card,
-- it can fail 3DS, it can be refunded — and a parallel table would need every
-- one of those columns and would drift from this one.
ALTER TABLE payments ADD COLUMN campaign_id uuid REFERENCES boost_campaigns (id);

ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_exactly_one_subject;
ALTER TABLE payments ADD CONSTRAINT payments_exactly_one_subject
    CHECK (num_nonnulls(order_id, package_purchase_id, campaign_id) = 1);

CREATE INDEX payments_campaign_idx ON payments (campaign_id)
    WHERE campaign_id IS NOT NULL;


COMMENT ON TABLE boost_targeting IS
    'Narrows where a reel appears. Never widens it past where the vendor delivers — the reel''s own coverage still applies.';
COMMENT ON TABLE boost_deliveries IS
    'Charged on confirmation, not on serve. A prefetched page that was never rendered must not bill anyone.';
COMMENT ON COLUMN boost_daily_spend.reserved_micros IS
    'Holds the gap between serving and confirmation. Without it a campaign one impression from its cap can be served five hundred times.';
COMMENT ON CONSTRAINT boost_campaigns_cannot_outspend_funding ON boost_campaigns IS
    'Prepaid, no credit line. Removes the entire category of weekend budget overruns.';
