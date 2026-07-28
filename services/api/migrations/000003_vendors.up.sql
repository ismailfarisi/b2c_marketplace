CREATE TABLE vendors (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            text NOT NULL,
    legal_name      text NOT NULL,
    trade_name      text NOT NULL,
    trade_name_ar   text,
    status          text NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'active', 'suspended', 'offboarded')),
    onboarding_stage text NOT NULL DEFAULT 'submitted',

    -- Compliance. A trade licence that has lapsed must block payouts, not just
    -- raise a dashboard warning — paying an unlicensed vendor is the platform's
    -- regulatory problem, not the vendor's.
    trade_licence_no      text,
    trade_licence_expires date,
    tax_registration_no   text,

    -- Denormalised rating cache. Recomputed by a job; never the source of truth.
    rating          numeric(3,2),
    rating_count    int NOT NULL DEFAULT 0,

    country         char(2) NOT NULL DEFAULT 'AE',
    default_currency char(3) NOT NULL DEFAULT 'AED',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT vendors_rating_range CHECK (rating IS NULL OR (rating >= 0 AND rating <= 5))
);

CREATE UNIQUE INDEX vendors_slug_key ON vendors (slug) WHERE deleted_at IS NULL;
CREATE INDEX vendors_status_idx ON vendors (status) WHERE deleted_at IS NULL;
CREATE INDEX vendors_trade_name_trgm ON vendors USING gin (trade_name gin_trgm_ops);
-- Payout gate: cheap to find every vendor whose licence lapses this month.
CREATE INDEX vendors_licence_expiry_idx ON vendors (trade_licence_expires)
    WHERE status = 'active' AND deleted_at IS NULL;

CREATE TRIGGER vendors_set_updated_at BEFORE UPDATE ON vendors
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Capacity, service areas and operating hours attach HERE, not to vendors.
-- A laundry chain has three facilities with independent throughput and
-- independent catchments; modelling capacity on the vendor makes that
-- unrepresentable and the mistake is expensive to undo later.
CREATE TABLE vendor_branches (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id   uuid NOT NULL REFERENCES vendors (id) ON DELETE CASCADE,
    name        text NOT NULL,
    name_ar     text,
    timezone    text NOT NULL DEFAULT 'Asia/Dubai',

    point       geography(Point, 4326) NOT NULL,
    -- The polygon this branch will actually serve. For services, eligibility is
    -- containment, not distance: a vendor 2 km away who does not cross the
    -- creek is not a candidate, and a radius filter would wrongly include them.
    service_area geography(MultiPolygon, 4326),

    address_line text,
    city        text,
    emirate     text,
    country     char(2) NOT NULL DEFAULT 'AE',

    phone       text,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE INDEX vendor_branches_vendor_idx ON vendor_branches (vendor_id) WHERE deleted_at IS NULL;
CREATE INDEX vendor_branches_point_idx ON vendor_branches USING gist (point);
CREATE INDEX vendor_branches_service_area_idx ON vendor_branches USING gist (service_area)
    WHERE service_area IS NOT NULL;

CREATE TRIGGER vendor_branches_set_updated_at BEFORE UPDATE ON vendor_branches
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Operating hours per branch per weekday. Ramadan and public holidays override
-- these via a separate calendar table (Phase 5) — in this region that is a
-- first-class scheduling concern, not an edge case.
CREATE TABLE branch_hours (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    -- 0 = Sunday. The GCC working week starts Sunday (UAE) or Sunday (KSA);
    -- ISO's Monday-first assumption produces off-by-one bugs in this market.
    weekday     smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    opens_at    time NOT NULL,
    closes_at   time NOT NULL,
    is_closed   boolean NOT NULL DEFAULT false,

    CONSTRAINT branch_hours_ordered CHECK (is_closed OR closes_at > opens_at),
    UNIQUE (branch_id, weekday, opens_at)
);

CREATE INDEX branch_hours_branch_idx ON branch_hours (branch_id);
