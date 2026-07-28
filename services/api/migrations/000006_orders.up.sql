CREATE TABLE orders (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Human-quotable. Support reads this over the phone; a UUID is unusable
    -- for that and customers will not read one out correctly.
    order_no    text NOT NULL,

    user_id     uuid NOT NULL REFERENCES users (id),
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id),
    vendor_id   uuid NOT NULL REFERENCES vendors (id),
    service_type text NOT NULL REFERENCES service_types (key),

    status      order_status NOT NULL DEFAULT 'pending_payment',
    kind        text NOT NULL DEFAULT 'one_off'
                    CHECK (kind IN ('one_off', 'subscription_generated')),
    -- Set on subscription-generated orders (Phase 5). Nullable now; the column
    -- exists from Phase 1 so the rolling scheduler does not require a migration
    -- on a table that will be large by then.
    source_subscription_id uuid,

    -- Attribution. The conversion signal Phase 3 is built to measure: did a
    -- reel actually produce a booking? The columns exist from Phase 1 so the
    -- answer is available the day the action layer ships, rather than starting
    -- from zero history.
    source_reel_id   uuid,
    source_action_id uuid,

    currency    char(3) NOT NULL DEFAULT 'AED',
    notes       text,

    placed_at   timestamptz NOT NULL DEFAULT now(),
    confirmed_at timestamptz,
    completed_at timestamptz,
    cancelled_at timestamptz,
    cancellation_reason text,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX orders_order_no_key ON orders (order_no);
CREATE INDEX orders_user_recent_idx ON orders (user_id, placed_at DESC);
CREATE INDEX orders_branch_status_idx ON orders (branch_id, status);
CREATE INDEX orders_vendor_recent_idx ON orders (vendor_id, placed_at DESC);
-- Conversion analysis reads this directly.
CREATE INDEX orders_source_reel_idx ON orders (source_reel_id)
    WHERE source_reel_id IS NOT NULL;

CREATE TRIGGER orders_set_updated_at BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Name and price are SNAPSHOTS, deliberately duplicated from service_offerings.
--
-- This is not denormalisation for speed. A vendor raising a price next week must
-- not retroactively change what a customer was charged last week, and an
-- offering that is soft-deleted must not make a historical order unreadable.
-- FK-only here would be a correctness bug that surfaces as a billing dispute.
CREATE TABLE order_lines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    offering_id uuid NOT NULL REFERENCES service_offerings (id),

    name_snapshot       text NOT NULL,
    name_ar_snapshot    text,
    unit_price_minor    bigint NOT NULL,
    currency            char(3) NOT NULL DEFAULT 'AED',
    tax_rate_bp_snapshot int NOT NULL,

    quantity    int NOT NULL CHECK (quantity > 0),
    -- Vertical-specific selections, validated by the service type's
    -- ItemSelection strategy before the row is written. The engine stores it;
    -- only the strategy interprets it.
    options     jsonb NOT NULL DEFAULT '{}'::jsonb,

    sort_order  int NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT order_lines_price_nonneg CHECK (unit_price_minor >= 0)
);

CREATE INDEX order_lines_order_idx ON order_lines (order_id);


-- Order number generator: short, unambiguous, human-readable.
--
-- Excludes I, O, 0, 1 — over the phone those are indistinguishable and support
-- will transcribe them wrong. Crockford-style alphabet for exactly that reason.
CREATE OR REPLACE FUNCTION generate_order_no(p_country char(2))
RETURNS text AS $$
DECLARE
    alphabet text := '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
    result text := '';
    i int;
BEGIN
    FOR i IN 1..6 LOOP
        result := result || substr(alphabet, 1 + floor(random() * length(alphabet))::int, 1);
    END LOOP;
    RETURN p_country || '-' || result;
END;
$$ LANGUAGE plpgsql;
