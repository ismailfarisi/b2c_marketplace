-- Four-kitchen rotation: one customer purchase, four weekly subscriptions.
--
-- The customer picks four kitchens for a 28-day period and pays once; the
-- platform decides which kitchen cooks which week. Each week is an ordinary
-- subscription with its own billing order, so the kitchen board, prep sheet,
-- ledger and payouts attribute every meal to the kitchen that cooked it
-- without knowing rotations exist. This table is only the umbrella that ties
-- the four together for the customer and for support.
CREATE TABLE meal_rotations (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rotation_no   text NOT NULL,
    user_id       uuid NOT NULL REFERENCES users (id),
    address_id    uuid NOT NULL REFERENCES addresses (id),

    status        text NOT NULL DEFAULT 'pending_payment' CHECK (status IN (
                      'pending_payment', 'active', 'cancelled', 'completed')),

    start_date    date NOT NULL,
    end_date      date NOT NULL,
    meal_slots    text[] NOT NULL,
    delivery_weekdays smallint[] NOT NULL,

    -- Sum of the four kitchens' weekly rotation prices, as quoted.
    amount_minor  bigint NOT NULL CHECK (amount_minor >= 0),
    currency      char(3) NOT NULL DEFAULT 'AED',

    cancelled_at  timestamptz,
    cancellation_reason text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT meal_rotations_dates_ordered CHECK (end_date >= start_date)
);

CREATE UNIQUE INDEX meal_rotations_no_key ON meal_rotations (rotation_no);
CREATE INDEX meal_rotations_user_idx ON meal_rotations (user_id, created_at DESC);

CREATE OR REPLACE FUNCTION generate_rotation_no()
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
    RETURN 'ROT-' || to_char(now(), 'YYMM') || '-' || v_suffix;
END;
$$;

ALTER TABLE subscriptions
    ADD COLUMN rotation_id   uuid REFERENCES meal_rotations (id),
    ADD COLUMN rotation_week smallint,
    ADD CONSTRAINT subscriptions_rotation_week_valid CHECK (
        (rotation_id IS NULL AND rotation_week IS NULL)
        OR (rotation_id IS NOT NULL AND rotation_week BETWEEN 1 AND 4));

CREATE UNIQUE INDEX subscriptions_rotation_week_key
    ON subscriptions (rotation_id, rotation_week) WHERE rotation_id IS NOT NULL;
-- Load balancing reads how many rotation weeks a kitchen already has.
CREATE INDEX subscriptions_rotation_branch_idx
    ON subscriptions (branch_id, start_date) WHERE rotation_id IS NOT NULL;

-- A rotation is paid with one gateway authorization recorded against each of
-- its four weekly orders, so the same provider reference now appears once per
-- order rather than once overall.
DROP INDEX IF EXISTS payments_provider_ref_key;
CREATE UNIQUE INDEX payments_provider_ref_key ON payments (provider, provider_ref, order_id)
    WHERE provider_ref IS NOT NULL;
