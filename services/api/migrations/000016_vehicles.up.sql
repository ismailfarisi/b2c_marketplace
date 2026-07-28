-- Phase 6 — Vehicles C2C.
--
-- SCOPE-EXTENSIONS §3 opens with the sentence this migration is organised
-- around: "your entire money module does not apply." No vendors, no capacity,
-- no scheduling, no fulfillment, no commission. The transaction closes offline
-- at an RTA counter and the platform never touches it.
--
-- What that leaves is a second *supply* engine sitting behind the same feed:
-- listings instead of offerings, leads instead of orders, listing fees instead
-- of commission. The reels tables above are reused unchanged — a seller reel is
-- a reel with author_type='user' — and that reuse is the phase's thesis.
--
-- 25 tables. SCOPE-EXTENSIONS budgeted +25 with a slightly different split:
-- `listing_videos` is dropped (reels already are the listing's video, and a
-- second video table would fork the moderation pipeline), `market_valuations`
-- is deferred (see §6 of the phase doc — we never observe a sale price, so a
-- valuation built here would be a valuation of asking prices), and
-- `dealer_profiles` is added because a dealer is not a vendor and had nowhere
-- to live.

-- ─────────────────────────────────────────────────────────────────────────────
-- 1. Taxonomy — reference data
-- ─────────────────────────────────────────────────────────────────────────────
--
-- SCOPE-EXTENSIONS is blunt about this: "reference data you buy or license,
-- don't hand-build". These tables are shaped for a licensed feed to land in.
-- The seed at the bottom is enough to run the product in one city and is not a
-- substitute for that feed — a hand-maintained trim table goes stale in one
-- model year and every stale row is a listing that cannot be filtered.

CREATE TABLE vehicle_makes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       text NOT NULL,
    name       text NOT NULL,
    name_ar    text,
    -- Country of origin drives a real buyer filter in this market ("Japanese
    -- brands only") and is not derivable from the name.
    country    char(2),
    logo_key   text,
    is_active  boolean NOT NULL DEFAULT true,
    sort_order int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (slug)
);

CREATE TABLE vehicle_models (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    make_id    uuid NOT NULL REFERENCES vehicle_makes (id) ON DELETE CASCADE,
    slug       text NOT NULL,
    name       text NOT NULL,
    name_ar    text,
    body_type  text CHECK (body_type IN ('sedan', 'suv', 'coupe', 'hatchback',
                                          'pickup', 'van', 'convertible', 'wagon',
                                          'crossover', 'other')),
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (make_id, slug)
);

CREATE INDEX vehicle_models_make_idx ON vehicle_models (make_id) WHERE is_active;

CREATE TABLE vehicle_trims (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id   uuid NOT NULL REFERENCES vehicle_models (id) ON DELETE CASCADE,
    name       text NOT NULL,
    name_ar    text,
    -- A trim exists for a range of model years. Open-ended on the right: the
    -- current generation has no end year until the next one is announced.
    year_from  smallint NOT NULL CHECK (year_from BETWEEN 1950 AND 2100),
    year_to    smallint CHECK (year_to IS NULL OR year_to BETWEEN 1950 AND 2100),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (model_id, name, year_from),
    CONSTRAINT vehicle_trims_year_range CHECK (year_to IS NULL OR year_to >= year_from)
);

CREATE INDEX vehicle_trims_model_idx ON vehicle_trims (model_id, year_from);

-- One row per trim, not a key/value bag.
--
-- Specs are a fixed, known set for this category and every one of them is a
-- filter a buyer actually uses. A jsonb bag would make "automatic, 6-cylinder,
-- under 3.0L" an unindexable scan, which is the single most common query shape
-- in car classifieds.
CREATE TABLE vehicle_specs (
    trim_id      uuid PRIMARY KEY REFERENCES vehicle_trims (id) ON DELETE CASCADE,
    engine_cc    int CHECK (engine_cc IS NULL OR engine_cc BETWEEN 0 AND 12000),
    cylinders    smallint CHECK (cylinders IS NULL OR cylinders BETWEEN 0 AND 16),
    transmission text CHECK (transmission IN ('automatic', 'manual', 'cvt', 'dct', 'other')),
    drivetrain   text CHECK (drivetrain IN ('fwd', 'rwd', 'awd', '4wd')),
    fuel_type    text CHECK (fuel_type IN ('petrol', 'diesel', 'hybrid', 'plug_in_hybrid', 'electric')),
    power_hp     int,
    torque_nm    int,
    seats        smallint,
    doors        smallint,
    created_at   timestamptz NOT NULL DEFAULT now()
);


-- ─────────────────────────────────────────────────────────────────────────────
-- 2. Sellers
-- ─────────────────────────────────────────────────────────────────────────────

-- A dealer is a user with a trade licence, not a vendor.
--
-- The temptation is to reuse `vendors` — it has a name, a status, a KYC story.
-- Resisting it matters: every vendor in this system has branches, service
-- areas, capacity buckets, offerings and a payable account, and a dealer has
-- none of those. A dealer row in `vendors` would be a row that every payout
-- run, every capacity query and every commission report has to remember to
-- exclude, and one of them eventually won't.
CREATE TABLE dealer_profiles (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    business_name  text NOT NULL,
    business_name_ar text,
    trade_licence_no text,
    trade_licence_expiry date,
    emirate        text,
    address_text   text,
    point          geography(Point, 4326),
    logo_key       text,
    status         text NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'active', 'suspended', 'rejected')),
    verified_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_user_id)
);

CREATE TRIGGER dealer_profiles_set_updated_at BEFORE UPDATE ON dealer_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ─────────────────────────────────────────────────────────────────────────────
-- 3. Listings
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE vehicle_listings (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Human-quotable, like order_no. Support and the seller both need to say it
    -- out loud, and buyers screenshot it.
    reference_no  text NOT NULL,

    seller_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seller_kind   text NOT NULL DEFAULT 'private'
                       CHECK (seller_kind IN ('private', 'dealer')),
    dealer_id     uuid REFERENCES dealer_profiles (id) ON DELETE SET NULL,

    -- Taxonomy. trim_id is nullable because sellers frequently cannot identify
    -- their own trim, and refusing the listing over it loses supply.
    make_id       uuid NOT NULL REFERENCES vehicle_makes (id),
    model_id      uuid NOT NULL REFERENCES vehicle_models (id),
    trim_id       uuid REFERENCES vehicle_trims (id),

    year          smallint NOT NULL CHECK (year BETWEEN 1950 AND 2100),
    mileage_km    int NOT NULL CHECK (mileage_km BETWEEN 0 AND 2000000),

    -- ── the attribute set, SCOPE-EXTENSIONS §3 ──────────────────────────────
    transmission  text CHECK (transmission IN ('automatic', 'manual', 'cvt', 'dct', 'other')),
    fuel_type     text CHECK (fuel_type IN ('petrol', 'diesel', 'hybrid', 'plug_in_hybrid', 'electric')),
    body_type     text CHECK (body_type IN ('sedan', 'suv', 'coupe', 'hatchback',
                                             'pickup', 'van', 'convertible', 'wagon',
                                             'crossover', 'other')),
    exterior_colour text,
    interior_colour text,
    doors         smallint,
    seats         smallint,

    -- Regional spec is first-class and NOT NULL.
    --
    -- SCOPE-EXTENSIONS: "spec alone swings GCC resale value 20–30%". A US-spec
    -- import and a GCC-spec car of the same year and mileage are different
    -- products — different air conditioning, different warranty, different
    -- resale. Allowing NULL here would let a seller omit the single most
    -- price-relevant fact about the car, and every omission reads as evasion.
    regional_spec text NOT NULL
                       CHECK (regional_spec IN ('gcc', 'us', 'japanese', 'european',
                                                 'canadian', 'chinese', 'other')),

    -- 17 characters, and the ISO 3779 alphabet excludes I, O and Q so they are
    -- never confused with 1 and 0. Nullable: a private seller may not want it
    -- public before a serious buyer appears, and the fraud detectors treat its
    -- absence as a signal rather than refusing the listing.
    vin           text,
    plate_emirate text,
    registration_expiry date,

    warranty_until  date,
    service_history text CHECK (service_history IN ('full_dealer', 'full_independent',
                                                     'partial', 'none', 'unknown')),
    accident_history text CHECK (accident_history IN ('none', 'minor', 'major', 'unknown')),
    owners_count  smallint CHECK (owners_count IS NULL OR owners_count >= 0),

    price_minor   bigint NOT NULL CHECK (price_minor > 0),
    currency      char(3) NOT NULL DEFAULT 'AED',
    is_negotiable boolean NOT NULL DEFAULT true,

    title         text,
    description   text,
    description_ar text,

    -- Location. A radius, not a polygon: a car has a place, not a service area.
    -- resolve_reel_coverage (below) turns this into the reel's coverage circle.
    emirate       text,
    city          text,
    point         geography(Point, 4326),
    radius_m      int NOT NULL DEFAULT 50000 CHECK (radius_m BETWEEN 1000 AND 300000),

    status        text NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'pending_review', 'active', 'paused',
                                          'sold', 'expired', 'rejected', 'removed')),
    rejection_reason text,

    -- Denormalised from listing_promotions because both are hot filters on the
    -- search path and a join per row to answer "is this featured" is the kind of
    -- thing that looks free until the result set is 40k rows.
    featured_until timestamptz,
    bumped_at      timestamptz,

    published_at  timestamptz,
    expires_at    timestamptz,
    sold_at       timestamptz,
    -- Seller-declared and unverifiable. Read §5 of the phase doc before putting
    -- this number in front of anyone: the platform never sees the transaction.
    sold_price_minor bigint,
    removed_at    timestamptz,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    -- Same shape as reels_published_needs_coverage: a listing that is live but
    -- has no location is invisible to every geo query and reads as a search bug
    -- rather than a data bug. Refuse it at write time.
    CONSTRAINT vehicle_listings_active_needs_point
        CHECK (status <> 'active' OR (point IS NOT NULL AND published_at IS NOT NULL)),
    CONSTRAINT vehicle_listings_sold_needs_time
        CHECK (status <> 'sold' OR sold_at IS NOT NULL),
    CONSTRAINT vehicle_listings_dealer_pair
        CHECK ((seller_kind = 'dealer') = (dealer_id IS NOT NULL)),
    CONSTRAINT vehicle_listings_vin_shape
        CHECK (vin IS NULL OR vin ~ '^[A-HJ-NPR-Z0-9]{17}$')
);

CREATE UNIQUE INDEX vehicle_listings_reference_key ON vehicle_listings (reference_no);

-- The search index. Geo first because every buyer query is bounded by distance;
-- btree_gist (000001) lets price ride along so a price-sorted radius query does
-- not fetch the whole circle and sort it.
CREATE INDEX vehicle_listings_geo_idx ON vehicle_listings
    USING gist (point, price_minor)
    WHERE status = 'active';

CREATE INDEX vehicle_listings_facet_idx ON vehicle_listings
    (make_id, model_id, year, price_minor)
    WHERE status = 'active';

-- Free text is the escape hatch, not the main path — "GTR", "شبح", the trim
-- nobody modelled. Trigrams rather than tsvector because this market types
-- brand names in two scripts and several spellings and a stemmer for neither
-- language helps.
CREATE INDEX vehicle_listings_title_trgm_idx ON vehicle_listings
    USING gin (title gin_trgm_ops)
    WHERE status = 'active';

CREATE INDEX vehicle_listings_seller_idx ON vehicle_listings (seller_id, created_at DESC);
CREATE INDEX vehicle_listings_dealer_idx ON vehicle_listings (dealer_id, created_at DESC)
    WHERE dealer_id IS NOT NULL;
-- Drives the expiry sweeper.
CREATE INDEX vehicle_listings_expiry_idx ON vehicle_listings (expires_at)
    WHERE status = 'active';
-- Drives odometer-rollback and duplicate detection. Not unique: the same car
-- legitimately gets relisted, and by a second owner after a real sale.
CREATE INDEX vehicle_listings_vin_idx ON vehicle_listings (vin)
    WHERE vin IS NOT NULL;

CREATE TRIGGER vehicle_listings_set_updated_at BEFORE UPDATE ON vehicle_listings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Photos are separate from reels on purpose.
--
-- A reel goes through transcode + moderation and is a feed object. A listing
-- photo is a still that appears on the listing page and never enters the feed.
-- Sharing one table would mean every photo upload paid for a video pipeline.
CREATE TABLE listing_photos (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id     uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    media_asset_id uuid REFERENCES media_assets (id) ON DELETE SET NULL,
    s3_key         text NOT NULL,
    position       int NOT NULL DEFAULT 0,
    -- Photos are moderated too — a listing photo is where a phone number
    -- written on the windscreen gets past a text scanner.
    state          text NOT NULL DEFAULT 'pending'
                        CHECK (state IN ('pending', 'approved', 'rejected')),
    reject_reason  text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (listing_id, position)
);

CREATE INDEX listing_photos_pending_idx ON listing_photos (created_at)
    WHERE state = 'pending';


CREATE TABLE listing_status_history (
    id          bigserial PRIMARY KEY,
    listing_id  uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    from_status text,
    to_status   text NOT NULL,
    actor_type  text CHECK (actor_type IN ('system', 'seller', 'moderator')),
    actor_id    uuid,
    reason      text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX listing_status_history_listing_idx
    ON listing_status_history (listing_id, created_at DESC);


-- Price history is a trust artefact, not an audit log.
--
-- A listing whose price drops 40% in three days is either a motivated seller or
-- a bait-and-switch, and buyers are entitled to see which. It is also the input
-- to the price-outlier fraud detector.
CREATE TABLE listing_price_history (
    id          bigserial PRIMARY KEY,
    listing_id  uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    price_minor bigint NOT NULL CHECK (price_minor > 0),
    currency    char(3) NOT NULL DEFAULT 'AED',
    changed_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX listing_price_history_listing_idx
    ON listing_price_history (listing_id, created_at DESC);


-- ─────────────────────────────────────────────────────────────────────────────
-- 4. Demand side
-- ─────────────────────────────────────────────────────────────────────────────

-- A lead is this vertical's conversion event.
--
-- It is the analogue of `orders` in the services engine and it is deliberately
-- NOT an order: no money, no fulfillment, no obligation. Phase 3 built honest
-- conversion measurement for orders; the honest version here stops at the lead,
-- because the sale happens at an RTA counter the platform cannot observe.
CREATE TABLE listing_leads (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id  uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    buyer_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seller_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    source      text NOT NULL DEFAULT 'listing_page'
                     CHECK (source IN ('reel_action', 'listing_page', 'search', 'saved_search')),
    -- The Phase 3 attribution chain, reused verbatim. A lead that came from a
    -- reel is the number this whole plan exists to produce.
    source_reel_id   uuid REFERENCES reels (id) ON DELETE SET NULL,
    source_action_id uuid,

    conversation_id uuid,
    state       text NOT NULL DEFAULT 'new'
                     CHECK (state IN ('new', 'contacted', 'negotiating', 'closed', 'lost')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- One lead per buyer per listing. Without it a buyer who taps twice doubles
    -- the seller's lead count, and lead count is what dealers pay for.
    UNIQUE (listing_id, buyer_id)
);

CREATE INDEX listing_leads_seller_idx ON listing_leads (seller_id, created_at DESC);
CREATE INDEX listing_leads_reel_idx ON listing_leads (source_reel_id)
    WHERE source_reel_id IS NOT NULL;

CREATE TRIGGER listing_leads_set_updated_at BEFORE UPDATE ON listing_leads
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Offers are non-binding and carry no money.
--
-- Escrow would solve most of this category's fraud, and it implies a
-- payment-institution licence (SCOPE-EXTENSIONS §3). Until that decision is
-- taken, an accepted offer is a stated intention: it creates no obligation, no
-- payment intent and no ledger entry. Anything that made it feel otherwise to a
-- buyer would be the most expensive UI copy in the product.
CREATE TABLE listing_offers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id   uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    conversation_id uuid,
    buyer_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency     char(3) NOT NULL DEFAULT 'AED',
    message      text,
    state        text NOT NULL DEFAULT 'pending'
                      CHECK (state IN ('pending', 'accepted', 'rejected', 'withdrawn', 'expired')),
    expires_at   timestamptz NOT NULL,
    responded_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX listing_offers_listing_idx ON listing_offers (listing_id, created_at DESC);
CREATE INDEX listing_offers_buyer_idx ON listing_offers (buyer_id, created_at DESC);
-- Drives the expiry sweeper. A pending offer with no expiry is a negotiation
-- that never ends.
CREATE INDEX listing_offers_pending_idx ON listing_offers (expires_at)
    WHERE state = 'pending';


-- The query is stored as jsonb because it is a filter set, not a row.
--
-- Normalising it would mean a column per facet and a migration per new facet,
-- and the alert matcher reads it as a whole rather than querying inside it.
CREATE TABLE saved_searches (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text,
    query       jsonb NOT NULL,
    point       geography(Point, 4326),
    radius_m    int CHECK (radius_m IS NULL OR radius_m BETWEEN 1000 AND 500000),
    frequency   text NOT NULL DEFAULT 'daily'
                     CHECK (frequency IN ('instant', 'daily', 'off')),
    last_run_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX saved_searches_due_idx ON saved_searches (frequency, last_run_at NULLS FIRST)
    WHERE frequency <> 'off';

CREATE TRIGGER saved_searches_set_updated_at BEFORE UPDATE ON saved_searches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- The UNIQUE is the entire table.
--
-- An alert log without it re-notifies on every run, the user turns alerts off,
-- and the highest-intent audience in the product is gone. Everything else here
-- is bookkeeping.
CREATE TABLE saved_search_alerts (
    id              bigserial PRIMARY KEY,
    saved_search_id uuid NOT NULL REFERENCES saved_searches (id) ON DELETE CASCADE,
    listing_id      uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    channel         text NOT NULL DEFAULT 'push'
                         CHECK (channel IN ('push', 'email', 'sms', 'none')),
    sent_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (saved_search_id, listing_id)
);


-- Distinct from reel_saves, and not a duplicate of it.
--
-- Saving a reel bookmarks a piece of content; favouriting a listing bookmarks a
-- car. A listing outlives the reel that advertised it and a reel can advertise
-- a listing that has sold, so collapsing the two would make "my saved cars"
-- show sold cars and "my saved videos" lose videos when a seller relists.
CREATE TABLE listing_favorites (
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    listing_id uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, listing_id)
);

CREATE INDEX listing_favorites_listing_idx ON listing_favorites (listing_id);


-- ─────────────────────────────────────────────────────────────────────────────
-- 5. Chat — required, not optional
-- ─────────────────────────────────────────────────────────────────────────────
--
-- SCOPE-EXTENSIONS §3: "exposing seller phone numbers invites spam and
-- advance-fee fraud". Chat exists so the phone number never has to be public.
-- That makes the message scanner (internal/modules/chat/scan.go) load-bearing:
-- a chat that passes contact details through in the first message provides the
-- fraud surface without the protection.

CREATE TABLE conversations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id  uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    buyer_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seller_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    state       text NOT NULL DEFAULT 'open'
                     CHECK (state IN ('open', 'archived', 'blocked', 'closed')),
    -- Unread counts are per side and denormalised. The alternative is a
    -- COUNT(*) over messages on every inbox render, which is the one screen a
    -- user opens most.
    buyer_unread  int NOT NULL DEFAULT 0 CHECK (buyer_unread >= 0),
    seller_unread int NOT NULL DEFAULT 0 CHECK (seller_unread >= 0),
    last_message_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- One thread per buyer per listing. A second thread about the same car is a
    -- split conversation neither party can follow.
    UNIQUE (listing_id, buyer_id),
    CONSTRAINT conversations_distinct_parties CHECK (buyer_id <> seller_id)
);

CREATE INDEX conversations_buyer_idx ON conversations (buyer_id, last_message_at DESC NULLS LAST);
CREATE INDEX conversations_seller_idx ON conversations (seller_id, last_message_at DESC NULLS LAST);


CREATE TABLE messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- body is what the sender wrote; delivered_body is what the recipient sees.
    -- Keeping both is what makes a redaction reviewable — if the scanner
    -- overreaches, there is something to compare against, and if a fraud case
    -- goes to a regulator the original is still there.
    body            text NOT NULL,
    delivered_body  text,
    state           text NOT NULL DEFAULT 'sent'
                         CHECK (state IN ('sent', 'redacted', 'held', 'blocked', 'deleted')),
    -- Comma-free list of scanner finding kinds, stored for the fraud detectors
    -- to aggregate over.
    findings        text[] NOT NULL DEFAULT '{}',
    review_reason   text,

    read_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX messages_conversation_idx ON messages (conversation_id, created_at DESC);
CREATE INDEX messages_held_idx ON messages (created_at) WHERE state = 'held';
-- Drives the burst-rate fraud signal: one account messaging fifty sellers in an
-- hour is not a buyer.
CREATE INDEX messages_sender_recent_idx ON messages (sender_id, created_at DESC);


CREATE TABLE message_attachments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id     uuid NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    media_asset_id uuid REFERENCES media_assets (id) ON DELETE SET NULL,
    s3_key         text NOT NULL,
    kind           text NOT NULL DEFAULT 'image'
                        CHECK (kind IN ('image', 'document')),
    state          text NOT NULL DEFAULT 'pending'
                        CHECK (state IN ('pending', 'approved', 'rejected')),
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX message_attachments_message_idx ON message_attachments (message_id);

-- Late FKs: leads and offers point at conversations, which are defined after
-- them because a conversation points at a listing. Declared here rather than
-- reordering the file, so each section still reads top-to-bottom.
ALTER TABLE listing_leads
    ADD CONSTRAINT listing_leads_conversation_fk
    FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE SET NULL;

ALTER TABLE listing_offers
    ADD CONSTRAINT listing_offers_conversation_fk
    FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE SET NULL;


-- ─────────────────────────────────────────────────────────────────────────────
-- 6. Trust & fraud
-- ─────────────────────────────────────────────────────────────────────────────
--
-- PLAN-VIDEO-FIRST §9 ranks "T&S underestimated" as a serious risk and names
-- this phase as when it stops being manageable by one part-time reviewer. These
-- five tables are the machinery; the headcount is the actual mitigation.

CREATE TABLE seller_verifications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind        text NOT NULL
                     CHECK (kind IN ('phone', 'emirates_id', 'trade_licence',
                                      'bank_account', 'selfie_match')),
    state       text NOT NULL DEFAULT 'pending'
                     CHECK (state IN ('pending', 'verified', 'failed', 'expired')),
    -- Never the document itself. A scan of an Emirates ID is the highest-value
    -- thing in this database to an attacker and it has no business being in
    -- Postgres; the reference points at an encrypted object with its own
    -- lifecycle and its own deletion schedule.
    document_ref text,
    provider    text,
    verified_at timestamptz,
    expires_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind)
);


-- The moat, if it can be integrated.
--
-- SCOPE-EXTENSIONS §3: Emirates Vehicle Gate / Absher lookups. The table is
-- shaped for that integration and is useful before it exists — a manually
-- entered report from an authorised centre lands in exactly this shape.
CREATE TABLE vehicle_history_checks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id  uuid REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    vin         text,
    provider    text NOT NULL,
    state       text NOT NULL DEFAULT 'pending'
                     CHECK (state IN ('pending', 'completed', 'unavailable', 'failed')),
    -- The three findings that matter, promoted out of the payload because the
    -- fraud detectors and the listing page both read them.
    accident_count   int,
    reported_mileage int,
    is_stolen        boolean,
    has_finance      boolean,
    raw_payload      jsonb,
    checked_at       timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX vehicle_history_checks_listing_idx ON vehicle_history_checks (listing_id);
CREATE INDEX vehicle_history_checks_vin_idx ON vehicle_history_checks (vin)
    WHERE vin IS NOT NULL;


CREATE TABLE inspection_reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id  uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    inspector   text NOT NULL,
    -- 0–100. A single number is what buyers actually compare; the detail lives
    -- in the report document.
    overall_score smallint CHECK (overall_score IS NULL OR overall_score BETWEEN 0 AND 100),
    report_key  text,
    summary     text,
    inspected_at timestamptz,
    expires_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX inspection_reports_listing_idx ON inspection_reports (listing_id, inspected_at DESC);


-- Evidence, not verdicts.
--
-- Same shape as interest signals in Phase 4: append rows, never mutate a score.
-- A signal that has been dismissed stays in the table with state='dismissed',
-- because "we saw this and decided it was fine" is a different fact from "we
-- never saw it" and only one of them survives an appeal.
CREATE TABLE fraud_signals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type text NOT NULL CHECK (subject_type IN ('listing', 'user', 'conversation', 'message')),
    subject_id   uuid NOT NULL,
    kind         text NOT NULL,
    -- 1–100. Severity is the detector's confidence in this instance; the weight
    -- of the *kind* lives in Go (internal/modules/trust/signals.go) so it can be
    -- retuned without a migration.
    severity     smallint NOT NULL DEFAULT 50 CHECK (severity BETWEEN 1 AND 100),
    source       text NOT NULL DEFAULT 'system'
                      CHECK (source IN ('system', 'moderator', 'report')),
    detail       jsonb,
    state        text NOT NULL DEFAULT 'open'
                      CHECK (state IN ('open', 'confirmed', 'dismissed')),
    resolved_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    resolved_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX fraud_signals_subject_idx ON fraud_signals (subject_type, subject_id, created_at DESC);
CREATE INDEX fraud_signals_open_idx ON fraud_signals (created_at) WHERE state = 'open';
-- One detector run must not write the same finding twice. Detectors are
-- idempotent by re-running the same query, so uniqueness has to come from here.
CREATE UNIQUE INDEX fraud_signals_dedupe_idx
    ON fraud_signals (subject_type, subject_id, kind)
    WHERE state = 'open';


CREATE TABLE trust_scores (
    subject_type text NOT NULL CHECK (subject_type IN ('listing', 'user')),
    subject_id   uuid NOT NULL,
    -- 0–100, higher is better. Derived from fraud_signals by a decaying fold —
    -- never incremented in place, so it can always be recomputed from evidence.
    score        smallint NOT NULL DEFAULT 100 CHECK (score BETWEEN 0 AND 100),
    band         text NOT NULL DEFAULT 'ok'
                      CHECK (band IN ('ok', 'watch', 'restricted', 'blocked')),
    signal_count int NOT NULL DEFAULT 0,
    computed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subject_type, subject_id)
);

CREATE INDEX trust_scores_band_idx ON trust_scores (band, computed_at)
    WHERE band <> 'ok';


-- ─────────────────────────────────────────────────────────────────────────────
-- 7. Monetisation — the classifieds model
-- ─────────────────────────────────────────────────────────────────────────────
--
-- No commission, because there is no GMV to take a percentage of. Revenue is
-- listing fees, bumps, features and dealer packages. Structurally this is the
-- first thing in the system the *platform itself* sells, which is why it does
-- not fit `orders` (that table requires a vendor, a branch and a service type)
-- and why `payments` had to be widened below.

CREATE TABLE listing_packages (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code          text NOT NULL,
    name          text NOT NULL,
    name_ar       text,
    audience      text NOT NULL DEFAULT 'private'
                       CHECK (audience IN ('private', 'dealer')),
    -- What the money buys. All three are entitlements, all three are consumable.
    listing_quota int NOT NULL DEFAULT 1 CHECK (listing_quota >= 0),
    duration_days int NOT NULL CHECK (duration_days BETWEEN 1 AND 730),
    featured_days int NOT NULL DEFAULT 0 CHECK (featured_days >= 0),
    bump_quota    int NOT NULL DEFAULT 0 CHECK (bump_quota >= 0),

    price_minor   bigint NOT NULL CHECK (price_minor >= 0),
    currency      char(3) NOT NULL DEFAULT 'AED',
    -- The platform is the supplier of record for a listing fee — unlike GMV,
    -- where the vendor is. That makes this VAT the platform's own output tax,
    -- and it is due at the earlier of invoice or payment, which is why
    -- PostListingFeeCharged books it at purchase rather than at recognition.
    tax_rate_bp   int NOT NULL DEFAULT 500 CHECK (tax_rate_bp BETWEEN 0 AND 10000),

    is_recurring  boolean NOT NULL DEFAULT false,
    is_active     boolean NOT NULL DEFAULT true,
    sort_order    int NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (code)
);


CREATE TABLE package_purchases (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_id    uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    dealer_id   uuid REFERENCES dealer_profiles (id) ON DELETE SET NULL,
    package_id  uuid NOT NULL REFERENCES listing_packages (id),

    -- Snapshot, for the same reason order_lines snapshots offering prices: a
    -- package repriced next month must not change what was charged last month.
    amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
    tax_minor    bigint NOT NULL DEFAULT 0 CHECK (tax_minor >= 0),
    currency     char(3) NOT NULL DEFAULT 'AED',
    listing_quota int NOT NULL,
    bump_quota    int NOT NULL DEFAULT 0,
    duration_days int NOT NULL,
    featured_days int NOT NULL DEFAULT 0,

    -- Consumption. Quota is decremented as listings publish and bumps are used;
    -- both are bounded by CHECKs so an over-spend fails at the database rather
    -- than being discovered in a reconciliation months later.
    listings_used int NOT NULL DEFAULT 0 CHECK (listings_used >= 0),
    bumps_used    int NOT NULL DEFAULT 0 CHECK (bumps_used >= 0),

    status       text NOT NULL DEFAULT 'pending_payment'
                      CHECK (status IN ('pending_payment', 'active', 'exhausted',
                                         'expired', 'refunded', 'cancelled')),
    starts_at    timestamptz,
    ends_at      timestamptz,

    -- Deferred revenue, recognised straight-line across the entitlement window.
    -- Identical arithmetic to a prepaid meal cycle (money.RecogniseThrough),
    -- which is the one piece of Phase 5 that turned out to be vertical-neutral.
    recognised_minor bigint NOT NULL DEFAULT 0 CHECK (recognised_minor >= 0),

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT package_purchases_quota_bounded
        CHECK (listings_used <= listing_quota AND bumps_used <= bump_quota),
    CONSTRAINT package_purchases_recognised_bounded
        CHECK (recognised_minor <= amount_minor),
    CONSTRAINT package_purchases_active_window
        CHECK (status <> 'active' OR (starts_at IS NOT NULL AND ends_at IS NOT NULL))
);

CREATE INDEX package_purchases_buyer_idx ON package_purchases (buyer_id, created_at DESC);
CREATE INDEX package_purchases_usable_idx ON package_purchases (buyer_id, ends_at)
    WHERE status = 'active';
-- Drives the recognition job.
CREATE INDEX package_purchases_recognise_idx ON package_purchases (ends_at)
    WHERE status IN ('active', 'exhausted');

CREATE TRIGGER package_purchases_set_updated_at BEFORE UPDATE ON package_purchases
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- A promotion is an entitlement spent on one listing.
--
-- Separate from package_purchases because one purchase feeds many promotions
-- and because "why is this listing at the top" needs an answer with a row
-- behind it. Marketplaces that cannot answer that question lose the trust of
-- the sellers who paid.
CREATE TABLE listing_promotions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id  uuid NOT NULL REFERENCES vehicle_listings (id) ON DELETE CASCADE,
    purchase_id uuid REFERENCES package_purchases (id) ON DELETE SET NULL,
    kind        text NOT NULL CHECK (kind IN ('feature', 'bump', 'highlight')),
    starts_at   timestamptz NOT NULL DEFAULT now(),
    ends_at     timestamptz,
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT listing_promotions_window CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX listing_promotions_listing_idx ON listing_promotions (listing_id, starts_at DESC);
CREATE INDEX listing_promotions_active_idx ON listing_promotions (ends_at)
    WHERE ends_at IS NOT NULL;


-- Dealer subscriptions are a recurring package purchase, and this table is a
-- duplicate of machinery Phase 5 already built.
--
-- That is stated plainly rather than hidden: `subscriptions` has cycles,
-- dunning, deferred revenue and proration, and all of it would apply here. It
-- is not reused because `subscription_plans` is bound to a vendor branch and a
-- service_type — Phase 5 built recurrence, then tied it to the fulfillment
-- axis. Unifying means splitting that table into a billing plan and a delivery
-- plan, which is a change to a live launch vertical and does not belong in this
-- phase. See §7 of the phase doc for the exact diff that would do it.
CREATE TABLE dealer_subscriptions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dealer_id     uuid NOT NULL REFERENCES dealer_profiles (id) ON DELETE CASCADE,
    package_id    uuid NOT NULL REFERENCES listing_packages (id),
    status        text NOT NULL DEFAULT 'pending_payment'
                       CHECK (status IN ('pending_payment', 'active', 'past_due',
                                          'suspended', 'cancelled')),
    current_purchase_id uuid REFERENCES package_purchases (id) ON DELETE SET NULL,
    renews_at     timestamptz,
    cancelled_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dealer_id)
);

CREATE TRIGGER dealer_subscriptions_set_updated_at BEFORE UPDATE ON dealer_subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- ─────────────────────────────────────────────────────────────────────────────
-- 8. Widening `payments` — the phase's one engine change
-- ─────────────────────────────────────────────────────────────────────────────
--
-- `payments.order_id` was NOT NULL, which encoded an assumption that held for
-- five phases: every payment settles an order, and every order has a vendor, a
-- branch and a service type. A listing fee has none of those. The platform is
-- selling something itself for the first time.
--
-- The two alternatives were both worse. A synthetic "platform vendor" row makes
-- every payout run, capacity query and commission report carry an exclusion it
-- will eventually forget. A second payment table forks the gateway integration,
-- the webhook handler and the reconciliation surface in two.
--
-- So the abstraction gets fixed instead: `payments` was named for what it is and
-- coupled to what it happened to be used for. PLAN.md §5 P2's gate says to stop
-- and fix the abstraction when a new vertical would otherwise force internals to
-- change — this is that, applied one phase late.
ALTER TABLE payments ALTER COLUMN order_id DROP NOT NULL;

ALTER TABLE payments
    ADD COLUMN package_purchase_id uuid REFERENCES package_purchases (id);

-- Exactly one subject. Neither is a payment against nothing; both is a payment
-- that two reconciliations would each claim.
ALTER TABLE payments
    ADD CONSTRAINT payments_exactly_one_subject
    CHECK (num_nonnulls(order_id, package_purchase_id) = 1);

CREATE INDEX payments_purchase_idx ON payments (package_purchase_id)
    WHERE package_purchase_id IS NOT NULL;


-- ─────────────────────────────────────────────────────────────────────────────
-- 9. Coverage for user-authored reels
-- ─────────────────────────────────────────────────────────────────────────────
--
-- The Phase 2 function had an `IF v_author_type = 'vendor_branch'` branch and no
-- other. That IF is the extension point, and this is the phase that uses it: a
-- seller reel's audience is a circle around the car, not a service-area polygon.
--
-- Note what does *not* change. No Go in the media pipeline, no feed query, no
-- ranking code, no index. The feed still selects on `coverage` and still does
-- not know what a car is.
CREATE OR REPLACE FUNCTION resolve_reel_coverage(p_reel_id uuid)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    v_author_type text;
    v_author_id   uuid;
    v_subject_type text;
    v_subject_id  uuid;
    v_coverage    geography;
    v_kind        text;
    v_origin      geography;
BEGIN
    SELECT author_type, author_id, subject_type, subject_id
      INTO v_author_type, v_author_id, v_subject_type, v_subject_id
      FROM reels WHERE id = p_reel_id;

    IF v_author_type IS NULL THEN
        RETURN false;
    END IF;

    IF v_author_type = 'vendor_branch' THEN
        SELECT b.service_area, b.point
          INTO v_coverage, v_origin
          FROM vendor_branches b
         WHERE b.id = v_author_id
           AND b.deleted_at IS NULL
           AND b.is_active;
        v_kind := 'service_area';

    ELSIF v_author_type = 'user' THEN
        -- A user-authored reel earns an audience from its subject, not from its
        -- author. A person has no service area, and a reel about nothing has no
        -- one to show it to — returning false here is what keeps general UGC out
        -- of the feed until a phase deliberately admits it.
        IF v_subject_type = 'vehicle_listing' AND v_subject_id IS NOT NULL THEN
            SELECT ST_Buffer(l.point, l.radius_m), l.point
              INTO v_coverage, v_origin
              FROM vehicle_listings l
             WHERE l.id = v_subject_id
               AND l.seller_id = v_author_id
               AND l.status IN ('draft', 'pending_review', 'active');
            v_kind := 'radius';
        END IF;
    END IF;

    IF v_coverage IS NULL THEN
        RETURN false;
    END IF;

    UPDATE reels
       SET coverage             = v_coverage,
           coverage_kind        = v_kind,
           origin_point         = v_origin,
           coverage_resolved_at = now()
     WHERE id = p_reel_id;

    RETURN true;
END;
$$;


-- Reference number generator, mirroring generate_order_no and
-- generate_subscription_no down to the alphabet: no 0, O, 1 or I, because these
-- get read over the phone and dictated into a WhatsApp message.
CREATE OR REPLACE FUNCTION generate_listing_reference()
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_alphabet text := '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
    v_suffix   text := '';
    i int;
BEGIN
    FOR i IN 1..6 LOOP
        v_suffix := v_suffix || substr(v_alphabet, 1 + floor(random() * length(v_alphabet))::int, 1);
    END LOOP;
    RETURN 'V-' || to_char(now(), 'YYMM') || '-' || v_suffix;
END;
$$;


-- ─────────────────────────────────────────────────────────────────────────────
-- 10. Seed
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Enough taxonomy to run one city, and the packages the pricing page needs.
-- Taxonomy is licensed reference data in production (see §1) — this is a
-- development fixture, not a catalogue.

INSERT INTO vehicle_makes (slug, name, name_ar, country, sort_order) VALUES
    ('toyota',        'Toyota',        'تويوتا',      'JP', 10),
    ('nissan',        'Nissan',        'نيسان',       'JP', 20),
    ('lexus',         'Lexus',         'لكزس',        'JP', 30),
    ('mitsubishi',    'Mitsubishi',    'ميتسوبيشي',   'JP', 40),
    ('honda',         'Honda',         'هوندا',       'JP', 50),
    ('hyundai',       'Hyundai',       'هيونداي',     'KR', 60),
    ('kia',           'Kia',           'كيا',         'KR', 70),
    ('mercedes-benz', 'Mercedes-Benz', 'مرسيدس بنز',  'DE', 80),
    ('bmw',           'BMW',           'بي إم دبليو', 'DE', 90),
    ('land-rover',    'Land Rover',    'لاند روفر',   'GB', 100),
    ('ford',          'Ford',          'فورد',        'US', 110),
    ('chevrolet',     'Chevrolet',     'شيفروليه',    'US', 120)
ON CONFLICT (slug) DO NOTHING;

INSERT INTO vehicle_models (make_id, slug, name, name_ar, body_type)
SELECT m.id, v.slug, v.name, v.name_ar, v.body_type
  FROM (VALUES
      ('toyota',        'land-cruiser', 'Land Cruiser', 'لاند كروزر', 'suv'),
      ('toyota',        'corolla',      'Corolla',      'كورولا',     'sedan'),
      ('toyota',        'camry',        'Camry',        'كامري',      'sedan'),
      ('toyota',        'hilux',        'Hilux',        'هايلكس',     'pickup'),
      ('nissan',        'patrol',       'Patrol',       'باترول',     'suv'),
      ('nissan',        'altima',       'Altima',       'التيما',     'sedan'),
      ('lexus',         'lx',           'LX',           'إل إكس',     'suv'),
      ('lexus',         'es',           'ES',           'إي إس',      'sedan'),
      ('mitsubishi',    'pajero',       'Pajero',       'باجيرو',     'suv'),
      ('honda',         'accord',       'Accord',       'أكورد',      'sedan'),
      ('hyundai',       'tucson',       'Tucson',       'توسان',      'crossover'),
      ('kia',           'sportage',     'Sportage',     'سبورتاج',    'crossover'),
      ('mercedes-benz', 'g-class',      'G-Class',      'جي كلاس',    'suv'),
      ('mercedes-benz', 'c-class',      'C-Class',      'سي كلاس',    'sedan'),
      ('bmw',           'x5',           'X5',           'إكس 5',      'suv'),
      ('bmw',           '5-series',     '5 Series',     'الفئة الخامسة', 'sedan'),
      ('land-rover',    'range-rover',  'Range Rover',  'رينج روفر',  'suv'),
      ('ford',          'explorer',     'Explorer',     'إكسبلورر',   'suv'),
      ('chevrolet',     'tahoe',        'Tahoe',        'تاهو',       'suv')
  ) AS v (make_slug, slug, name, name_ar, body_type)
  JOIN vehicle_makes m ON m.slug = v.make_slug
ON CONFLICT (make_id, slug) DO NOTHING;

-- Packages.
--
-- The free tier exists because supply is the constraint in a cold-start
-- classifieds market. Charging a private seller to list their only car before
-- there are buyers on the platform is how the listing count stays at zero.
INSERT INTO listing_packages
    (code, name, name_ar, audience, listing_quota, duration_days, featured_days,
     bump_quota, price_minor, is_recurring, sort_order)
VALUES
    ('free',         'Free listing',   'إعلان مجاني',   'private', 1,  30, 0, 0,      0, false, 10),
    ('private-plus', 'Featured',       'إعلان مميز',    'private', 1,  60, 7, 3,  14900, false, 20),
    ('dealer-20',    'Dealer 20',      'تاجر 20',       'dealer',  20, 30, 5, 20, 99900, true,  30),
    ('dealer-100',   'Dealer 100',     'تاجر 100',      'dealer', 100, 30, 30, 100, 349900, true, 40)
ON CONFLICT (code) DO NOTHING;


COMMENT ON TABLE vehicle_listings IS
    'Classifieds, not commerce. No fulfillment, no capacity, no commission — the sale closes offline.';
COMMENT ON TABLE listing_leads IS
    'This vertical''s conversion event. The funnel ends here; anything past a lead is seller-reported.';
COMMENT ON COLUMN vehicle_listings.sold_price_minor IS
    'Seller-declared. The platform never observes the transaction. Never quote as a market price.';
COMMENT ON TABLE fraud_signals IS
    'Append-only evidence. Dismissed signals are kept — "we looked and it was fine" survives an appeal.';
