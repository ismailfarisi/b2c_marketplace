-- Property.
--
-- The fifth subject type, and the first one whose moderation posture is set by
-- law rather than by product judgement.
--
-- # Why this is not vehicle_listings with different columns
--
-- The two look alike from a distance: a private party lists an expensive thing,
-- buyers message, a moderator screens it. They are not alike in the one place
-- that decides the schema.
--
-- In this market a property advertisement is a *regulated publication*. Dubai
-- requires a Trakheesi permit per advert, obtained by a brokerage against a
-- specific listing, and the permit number must be displayed in the advert
-- itself. Abu Dhabi requires a DARI permit. Advertising without one is an
-- offence committed by the platform as much as by the advertiser, and the
-- penalty lands on whoever published it.
--
-- So the permit is not an optional trust badge in the way a mulkiya photo is for
-- a car. It is a precondition of publication, it belongs in a NOT NULL column
-- once live, and the moderation queue for this vertical is checking a number
-- against a register rather than judging whether a photo looks honest. That is a
-- different job for a different reviewer, which is why it gets its own queue.

CREATE TABLE property_listings (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_no text NOT NULL UNIQUE DEFAULT generate_listing_reference(),

    -- Who is advertising, and under whose licence.
    --
    -- advertiser_id is the account. broker_id is the *firm* whose permit this
    -- advert runs under, and it is nullable only for the `owner` case — a
    -- landlord advertising their own property still needs a permit in Dubai, but
    -- obtains it differently, and conflating the two would let an agent publish
    -- with no firm attached.
    advertiser_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    advertiser_kind text NOT NULL DEFAULT 'agent'
        CHECK (advertiser_kind IN ('agent', 'owner')),
    broker_id      uuid REFERENCES vendors (id) ON DELETE SET NULL,

    -- The regulator's numbers. Free text because they are issued by four
    -- different emirate authorities in three formats, and a schema that only
    -- accepted Dubai's would silently exclude the rest of the country.
    permit_number  text,
    permit_issuer  text CHECK (permit_issuer IS NULL OR permit_issuer IN
        ('trakheesi', 'dari', 'other')),
    permit_expires date,
    -- Broker Registration Number, the individual agent's licence. Displayed
    -- alongside the permit in a compliant advert.
    agent_brn      text,

    purpose text NOT NULL CHECK (purpose IN ('sale', 'rent')),
    property_type text NOT NULL CHECK (property_type IN (
        'apartment', 'villa', 'townhouse', 'penthouse', 'studio',
        'office', 'shop', 'warehouse', 'land'
    )),

    -- Zero bedrooms is a studio and is a real answer, so the column is NOT NULL
    -- with a zero rather than nullable — "unspecified" is not a thing a buyer
    -- can filter on.
    bedrooms   smallint NOT NULL DEFAULT 0 CHECK (bedrooms BETWEEN 0 AND 20),
    bathrooms  smallint NOT NULL DEFAULT 1 CHECK (bathrooms BETWEEN 0 AND 20),
    size_sqft  integer  NOT NULL CHECK (size_sqft BETWEEN 100 AND 1000000),

    furnishing text CHECK (furnishing IS NULL OR furnishing IN
        ('furnished', 'semi_furnished', 'unfurnished')),

    price_minor bigint NOT NULL CHECK (price_minor > 0),
    currency    char(3) NOT NULL DEFAULT 'AED',
    -- Rent is quoted per year here and per month almost everywhere else in the
    -- world. Storing the period rather than normalising means the advert shows
    -- the number the market uses, which is the one the tenant is comparing.
    rent_period text CHECK (rent_period IS NULL OR rent_period IN ('yearly', 'monthly', 'weekly', 'daily')),
    CONSTRAINT property_rent_has_period CHECK (
        (purpose = 'rent' AND rent_period IS NOT NULL) OR
        (purpose = 'sale' AND rent_period IS NULL)
    ),

    title       text,
    description text,

    -- Where. `community` is the name people actually search — "Dubai Marina",
    -- "Arabian Ranches" — and it is not derivable from the point.
    emirate   text,
    community text,
    tower     text,
    point     geography(Point, 4326) NOT NULL,

    status text NOT NULL DEFAULT 'draft' CHECK (status IN (
        'draft', 'pending_review', 'active', 'paused',
        'let_or_sold', 'expired', 'rejected', 'removed'
    )),
    rejection_reason text,

    published_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- The gate, expressed as a constraint rather than as a code path.
    --
    -- A listing may exist without a permit — that is what `draft` is for, and an
    -- agent gathering photos before the permit comes back is the normal
    -- sequence. It may not be *live* without one. Putting this in the table
    -- rather than only in the service means no admin tool, no backfill and no
    -- future handler can publish an unpermitted advert by accident, which is the
    -- one failure here that is a legal problem rather than a product one.
    CONSTRAINT property_live_needs_permit CHECK (
        status <> 'active' OR (permit_number IS NOT NULL AND permit_issuer IS NOT NULL)
    )
);

CREATE INDEX property_listings_advertiser_idx ON property_listings (advertiser_id, created_at DESC);
CREATE INDEX property_listings_live_idx ON property_listings (published_at DESC)
    WHERE status = 'active';
CREATE INDEX property_listings_geo_idx ON property_listings USING gist (point)
    WHERE status = 'active';
-- The review queue reads this. Partial, because the queue is a handful of rows
-- against a table that will be millions.
CREATE INDEX property_listings_queue_idx ON property_listings (created_at)
    WHERE status = 'pending_review';
-- Duplicate-advert detection: the same permit on two live listings is either a
-- mistake or a brokerage inflating its apparent inventory, and both are worth
-- surfacing. Not unique — a permit legitimately covers a relist after expiry.
CREATE INDEX property_listings_permit_idx ON property_listings (permit_number)
    WHERE permit_number IS NOT NULL;

CREATE TRIGGER property_listings_set_updated_at
    BEFORE UPDATE ON property_listings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Status history, matching what vehicles and reels keep and for the same
-- reason: when a regulator or an advertiser asks why a listing came down, the
-- answer has to be a row rather than a recollection.
CREATE TABLE property_status_history (
    id         bigserial PRIMARY KEY,
    listing_id uuid NOT NULL REFERENCES property_listings (id) ON DELETE CASCADE,
    from_state text,
    to_state   text NOT NULL,
    actor_type text NOT NULL CHECK (actor_type IN ('advertiser', 'moderator', 'system')),
    actor_id   uuid,
    reason     text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX property_status_history_listing_idx
    ON property_status_history (listing_id, created_at DESC);

-- The polymorphic CHECKs learn the fifth type and its CTA.
ALTER TABLE reels DROP CONSTRAINT IF EXISTS reels_subject_type_check;
ALTER TABLE reels ADD CONSTRAINT reels_subject_type_check
    CHECK (subject_type = ANY (ARRAY[
        'service_offering', 'meal_plan', 'vehicle_listing',
        'flash_offer', 'property_listing'
    ]));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_kind_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_kind_check
    CHECK (kind = ANY (ARRAY[
        'book_service', 'subscribe_plan', 'inquire_listing',
        'claim_offer', 'enquire_property',
        'open_menu', 'follow', 'share', 'external'
    ]));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_target_type_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_target_type_check
    CHECK (target_type = ANY (ARRAY[
        'service_offering', 'meal_plan', 'vehicle_listing',
        'flash_offer', 'property_listing',
        'vendor_branch', 'vendor', 'url'
    ]));
