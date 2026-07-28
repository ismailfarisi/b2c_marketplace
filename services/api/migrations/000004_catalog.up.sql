-- The strategy registry. This table is the architecture: each row binds the
-- four strategies that define a vertical. Adding laundry, meals, cleaning or a
-- salon is a row here plus Go implementations of the four interfaces in
-- internal/seam/servicetype — never a change to ordering, pricing, capacity or
-- logistics code.
--
-- Health metric (DATA-MODEL.md): a new *service* vertical should cost 0-2 new
-- tables. If it costs more, the abstraction has leaked and that is worth
-- stopping for.
CREATE TABLE service_types (
    key                 text PRIMARY KEY,
    name                text NOT NULL,
    name_ar             text,

    -- The four strategy bindings. Text, not enums: registering a vertical must
    -- be a data change plus a code deploy, not a schema migration.
    item_selection      text NOT NULL,
    fulfillment_pattern text NOT NULL,
    capacity_resource   text NOT NULL,
    pricing_strategy    text NOT NULL,

    -- False when the final amount is only known after handling — laundry by
    -- weight. Drives the authorize-with-headroom payment path rather than an
    -- immediate capture.
    price_final_at_booking boolean NOT NULL DEFAULT true,

    is_active           boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE service_types IS
    'Strategy registry. One row per vertical; binds item selection, fulfillment pattern, capacity resource and pricing.';


CREATE TABLE service_offerings (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    service_type    text NOT NULL REFERENCES service_types (key),

    name            text NOT NULL,
    name_ar         text,
    description     text,
    description_ar  text,

    -- Money is an integer in minor units, always. There is no float anywhere in
    -- this schema; 0.1 + 0.2 is not a debate anyone should have about a bill.
    base_price_minor bigint NOT NULL,
    currency        char(3) NOT NULL DEFAULT 'AED',
    -- True when base_price is an estimate the customer must not read as a total.
    price_is_estimate boolean NOT NULL DEFAULT false,
    -- VAT is displayed tax-inclusive in the GCC. tax_total is extracted FROM
    -- the total, not added to it, and getting this backwards misprices every
    -- item by the VAT rate.
    tax_rate_bp     int NOT NULL DEFAULT 500,

    lead_time_minutes int NOT NULL DEFAULT 0,
    image_url       text,
    sort_order      int NOT NULL DEFAULT 0,
    is_active       boolean NOT NULL DEFAULT true,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT offerings_price_nonneg CHECK (base_price_minor >= 0),
    CONSTRAINT offerings_tax_rate_sane CHECK (tax_rate_bp BETWEEN 0 AND 10000)
);

CREATE INDEX offerings_branch_active_idx ON service_offerings (branch_id)
    WHERE is_active AND deleted_at IS NULL;
CREATE INDEX offerings_service_type_idx ON service_offerings (service_type);
CREATE INDEX offerings_name_trgm ON service_offerings USING gin (name gin_trgm_ops);

CREATE TRIGGER service_offerings_set_updated_at BEFORE UPDATE ON service_offerings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Seed the walking-skeleton vertical. Laundry is the Phase 1 *engineering*
-- milestone: two fulfillments, an adjustable price, a real ledger entry. It is
-- the cheapest way to prove the engine end to end.
--
-- It is not a launch decision — see docs/README.md open decision #3.
INSERT INTO service_types
    (key, name, name_ar, item_selection, fulfillment_pattern, capacity_resource, pricing_strategy, price_final_at_booking)
VALUES
    ('laundry_wash_fold', 'Wash & Fold', 'غسيل وطي',
     'garment_count', 'pickup_delivery', 'pickup_slot', 'weight_based', false),
    ('laundry_dry_clean', 'Dry Cleaning', 'تنظيف جاف',
     'garment_count', 'pickup_delivery', 'pickup_slot', 'per_item', true);
