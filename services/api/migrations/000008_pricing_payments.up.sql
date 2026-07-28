-- Price as a state machine, versioned.
--
--   estimated -> authorized -> adjusted -> captured -> refunded
--
-- Laundry priced by weight genuinely moves through `adjusted` after pickup.
-- Modelling that as a first-class state rather than an exception is what stops
-- "amount changed after the customer paid" from becoming a special case bolted
-- onto every downstream consumer.
--
-- Rows are never mutated in place across states — a new version is written and
-- the old one superseded, so the full price history of an order is recoverable.
CREATE TABLE price_quotes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    uuid NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
    version     int NOT NULL DEFAULT 1,
    state       quote_state NOT NULL DEFAULT 'estimated',

    currency        char(3) NOT NULL DEFAULT 'AED',
    subtotal_minor  bigint NOT NULL DEFAULT 0,
    discount_minor  bigint NOT NULL DEFAULT 0,
    delivery_minor  bigint NOT NULL DEFAULT 0,
    surcharge_minor bigint NOT NULL DEFAULT 0,
    tax_minor       bigint NOT NULL DEFAULT 0,
    total_minor     bigint NOT NULL DEFAULT 0,

    -- True in every GCC market. Displayed prices already contain VAT, so
    -- tax_minor is the portion EXTRACTED FROM total_minor, not added to it.
    -- Reading this backwards misprices every line by the VAT rate, and it is
    -- the single most common way a Western-built billing engine gets the region
    -- wrong.
    tax_inclusive   boolean NOT NULL DEFAULT true,

    -- Authorization headroom for estimate-priced verticals. Authorize the
    -- estimate plus a margin, capture the true amount. Capturing more than was
    -- authorized fails at the gateway, and re-authorizing later fails because
    -- the customer has left.
    authorized_minor bigint,

    adjustment_reason text,
    superseded_by    uuid REFERENCES price_quotes (id),

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT quotes_total_nonneg CHECK (total_minor >= 0),
    UNIQUE (order_id, version)
);

-- One live quote per order. Two rows both claiming to be current is how an
-- order gets charged twice.
CREATE UNIQUE INDEX price_quotes_current_idx ON price_quotes (order_id)
    WHERE superseded_by IS NULL;

CREATE TRIGGER price_quotes_set_updated_at BEFORE UPDATE ON price_quotes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Tax lives at the LINE, not the quote. VAT treatment differs by item class —
-- a delivery fee, a tip and a garment are not necessarily rated alike, and
-- ZATCA e-invoicing (KSA) requires per-line tax detail. A single quote-level
-- tax figure cannot be decomposed back into that.
CREATE TABLE quote_lines (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    quote_id    uuid NOT NULL REFERENCES price_quotes (id) ON DELETE CASCADE,
    order_line_id uuid REFERENCES order_lines (id) ON DELETE SET NULL,

    kind        text NOT NULL CHECK (kind IN ('item', 'delivery', 'surcharge', 'discount', 'tax', 'tip')),
    label       text NOT NULL,
    label_ar    text,
    amount_minor bigint NOT NULL,
    tax_rate_bp int,
    tax_minor   bigint NOT NULL DEFAULT 0,
    sort_order  int NOT NULL DEFAULT 0
);

CREATE INDEX quote_lines_quote_idx ON quote_lines (quote_id);


CREATE TABLE payments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    uuid NOT NULL REFERENCES orders (id),
    quote_id    uuid REFERENCES price_quotes (id),
    user_id     uuid NOT NULL REFERENCES users (id),

    kind        payment_kind NOT NULL,
    status      payment_status NOT NULL DEFAULT 'pending',

    provider    text NOT NULL,
    -- The gateway's id. Unique per provider so a webhook replay is idempotent
    -- by construction rather than by application logic that might be skipped.
    provider_ref text,
    provider_intent_id text,

    amount_minor bigint NOT NULL,
    currency    char(3) NOT NULL DEFAULT 'AED',

    -- 3DS is mandatory in the region; the status matters for dispute liability
    -- and is worth storing explicitly rather than buried in a raw payload blob.
    three_ds_status text,
    failure_code    text,
    failure_message text,

    -- The gateway's raw response, kept for reconciliation and disputes. Never
    -- read by business logic — anything the platform depends on gets its own
    -- column.
    raw_payload jsonb,

    -- Refunds and captures point at the authorization they derive from.
    parent_payment_id uuid REFERENCES payments (id),

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    settled_at  timestamptz,

    CONSTRAINT payments_amount_positive CHECK (amount_minor > 0)
);

CREATE UNIQUE INDEX payments_provider_ref_key ON payments (provider, provider_ref)
    WHERE provider_ref IS NOT NULL;
CREATE INDEX payments_order_idx ON payments (order_id);
CREATE INDEX payments_user_recent_idx ON payments (user_id, created_at DESC);
CREATE INDEX payments_pending_idx ON payments (created_at)
    WHERE status IN ('pending', 'requires_action');

CREATE TRIGGER payments_set_updated_at BEFORE UPDATE ON payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
