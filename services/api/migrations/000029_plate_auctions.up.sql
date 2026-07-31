-- Vehicle plate auctions.
--
-- The vertical `ModeCountdown` was designed for and has never had. Three of the
-- seam's four time modes have been in production since the flash-offer and
-- property work; this is the fourth, and it is the one the mode's own comment
-- uses as its worked example:
--
--     "A plate auction opens nine days before it closes. Drawing the bar across
--      those nine days means it does not visibly move during the two minutes
--      anyone is watching, which is worse than no bar."
--
-- # Why a plate rather than a car
--
-- A distinctive number plate in the UAE is a genuine auction good and a used
-- Corolla is not. It is fungible in the way an auction needs — every bidder
-- wants the identical thing, so the only question is price — it has no
-- condition to inspect, no mileage to argue about and no test drive. Emirates
-- Auction has run exactly this for years, so the format is familiar to the
-- people this product is for.
--
-- It is also the one classifieds good where a countdown is *true* rather than
-- decorative. A car listing with a timer on it is a marketing device; a lot
-- that genuinely closes at a moment is a different product, and pretending
-- otherwise is how a feed's clocks stop being believed.

CREATE TABLE plate_auctions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_no  text NOT NULL UNIQUE DEFAULT generate_listing_reference(),

    seller_id     uuid NOT NULL REFERENCES users(id),

    -- The plate itself. Code and number apart, because that is how it is
    -- searched, sorted and valued: "AA 5" and "AA 500000" share a code and
    -- nothing else, and a single "AA 5" text column cannot be ordered by digit
    -- count — which is the first thing a bidder filters on.
    emirate       text NOT NULL,
    plate_code    text NOT NULL,
    plate_number  text NOT NULL,
    -- Denormalised for ordering and display. A plate's value is driven more by
    -- how few digits it has than by which digits they are.
    digit_count   smallint NOT NULL CHECK (digit_count BETWEEN 1 AND 6),

    currency      text NOT NULL DEFAULT 'AED',

    -- The floor a bid must clear to start. Distinct from the reserve.
    opening_minor bigint NOT NULL CHECK (opening_minor > 0),

    -- The price below which the seller will not sell.
    --
    -- Nullable, and *never* shown to bidders — only whether it has been met.
    -- Publishing a reserve turns an auction into a fixed-price sale with extra
    -- steps: nobody bids below it and nobody bids above it either.
    reserve_minor bigint CHECK (reserve_minor IS NULL OR reserve_minor > 0),

    -- The smallest legal raise. A fixed step rather than a percentage, because
    -- a percentage on a five-million-dirham plate produces increments nobody
    -- can type and on a two-thousand-dirham plate produces a penny auction.
    increment_minor bigint NOT NULL DEFAULT 100000 CHECK (increment_minor > 0),

    opens_at      timestamptz NOT NULL,
    closes_at     timestamptz NOT NULL,

    -- draft → scheduled → live → (closed_won | closed_unsold) → settled
    -- and cancelled from anywhere before it closes.
    status        text NOT NULL DEFAULT 'draft'
                  CHECK (status IN ('draft', 'scheduled', 'live',
                                    'closed_won', 'closed_unsold',
                                    'settled', 'cancelled')),

    -- Kept on the auction rather than derived from plate_bids on every read.
    -- The feed asks for the current price on every impression, and a MAX() over
    -- a bid table is a sequential scan per reel at exactly the moment the
    -- auction is busiest.
    current_minor bigint,
    current_bidder uuid REFERENCES users(id),
    bid_count     integer NOT NULL DEFAULT 0,

    winner_id     uuid REFERENCES users(id),

    point         geography(Point, 4326),

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT plate_auctions_closes_after_opens CHECK (closes_at > opens_at),

    -- A reserve below the opening bid is meaningless — the first legal bid
    -- would meet it — and is almost always a seller confusing the two fields.
    CONSTRAINT plate_auctions_reserve_above_opening CHECK (
        reserve_minor IS NULL OR reserve_minor >= opening_minor
    ),

    -- A won auction has a winner and a price; an unsold one has neither.
    -- Enforced here because the settlement path is the one place a bug would
    -- produce a plate sold to nobody.
    CONSTRAINT plate_auctions_won_has_winner CHECK (
        status <> 'closed_won' OR (winner_id IS NOT NULL AND current_minor IS NOT NULL)
    )
);

-- One live auction per plate. A seller listing the same plate twice is either a
-- mistake or an attempt to run a shill auction against themselves.
CREATE UNIQUE INDEX plate_auctions_one_live
    ON plate_auctions (emirate, plate_code, plate_number)
    WHERE status IN ('draft', 'scheduled', 'live');

-- The sweep that opens and closes lots reads exactly these two.
CREATE INDEX plate_auctions_due_to_open
    ON plate_auctions (opens_at) WHERE status = 'scheduled';
CREATE INDEX plate_auctions_due_to_close
    ON plate_auctions (closes_at) WHERE status = 'live';

CREATE INDEX plate_auctions_seller ON plate_auctions (seller_id, created_at DESC);

CREATE TABLE plate_bids (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auction_id  uuid NOT NULL REFERENCES plate_auctions(id) ON DELETE CASCADE,
    bidder_id   uuid NOT NULL REFERENCES users(id),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),

    -- Whether this bid extended the auction. Recorded rather than inferred,
    -- because "why did this lot close four minutes late" is the single most
    -- common question about any auction with anti-sniping, and reconstructing
    -- the answer from timestamps afterwards is guesswork.
    extended_close boolean NOT NULL DEFAULT false,

    placed_at   timestamptz NOT NULL DEFAULT now()
);

-- The bid history, newest first, and the uniqueness that stops a double-tap
-- becoming two bids.
CREATE INDEX plate_bids_auction ON plate_bids (auction_id, amount_minor DESC);
CREATE UNIQUE INDEX plate_bids_no_duplicate_amount
    ON plate_bids (auction_id, amount_minor);

CREATE TRIGGER plate_auctions_updated_at
    BEFORE UPDATE ON plate_auctions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The seam's registries.
ALTER TABLE reels DROP CONSTRAINT IF EXISTS reels_subject_type_check;
ALTER TABLE reels ADD CONSTRAINT reels_subject_type_check
    CHECK (subject_type IS NULL OR subject_type IN (
        'service_offering', 'meal_plan', 'vehicle_listing',
        'flash_offer', 'property_listing', 'plate_auction'
    ));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_kind_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_kind_check
    CHECK (kind IN (
        'book_service', 'subscribe_plan', 'inquire_listing', 'open_menu',
        'follow', 'share', 'external', 'claim_offer', 'enquire_property',
        'place_bid'
    ));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_target_type_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_target_type_check
    CHECK (target_type IN (
        'service_offering', 'meal_plan', 'vehicle_listing', 'vendor_branch',
        'reel', 'url', 'flash_offer', 'property_listing', 'plate_auction'
    ));

-- Coverage, for a reel about a plate.
--
-- Nationwide rather than a radius, and it is the only subject in the product
-- that gets one. Every other vertical is local because the thing is: a car has
-- to be driven to, a flat has to be lived in, a meal has to be delivered. A
-- plate is a registration transferred at an RTA counter, and a bidder in Al Ain
-- is exactly as able to win one as a bidder in Deira. Restricting it to a
-- radius would be a smaller auction for no reason.
CREATE OR REPLACE FUNCTION resolve_reel_coverage(p_reel_id uuid)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    v_author_type  text;
    v_author_id    uuid;
    v_subject_type text;
    v_subject_id   uuid;
    v_coverage     geography;
    v_kind         text;
    v_origin       geography;
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
        IF v_subject_type = 'vehicle_listing' AND v_subject_id IS NOT NULL THEN
            SELECT ST_Buffer(l.point, l.radius_m), l.point
              INTO v_coverage, v_origin
              FROM vehicle_listings l
             WHERE l.id = v_subject_id
               AND l.seller_id = v_author_id
               AND l.status IN ('draft', 'pending_review', 'active');
            v_kind := 'radius';

        ELSIF v_subject_type = 'property_listing' AND v_subject_id IS NOT NULL THEN
            SELECT ST_Buffer(p.point, 25000), p.point
              INTO v_coverage, v_origin
              FROM property_listings p
             WHERE p.id = v_subject_id
               AND p.advertiser_id = v_author_id
               AND p.status IN ('draft', 'pending_review', 'active');
            v_kind := 'radius';

        ELSIF v_subject_type = 'plate_auction' AND v_subject_id IS NOT NULL THEN
            -- The whole country. See the note above the function.
            --
            -- 'global' rather than a new coverage_kind: the column already has
            -- a CHECK naming exactly three values, and 'global' is what this
            -- is. Inventing 'nationwide' would have been a fourth value, a
            -- constraint change, and two words for one idea.
            SELECT ST_Buffer(COALESCE(a.point,
                       ST_SetSRID(ST_MakePoint(55.2708, 25.2048), 4326)::geography),
                   400000),
                   a.point
              INTO v_coverage, v_origin
              FROM plate_auctions a
             WHERE a.id = v_subject_id
               AND a.seller_id = v_author_id
               AND a.status IN ('draft', 'scheduled', 'live');
            v_kind := 'global';
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
