-- A customer may change the kitchen of one future rotation week. The old
-- week's subscription is cancelled and a replacement takes its rotation_week,
-- so the one-subscription-per-week index only counts live subscriptions.
DROP INDEX subscriptions_rotation_week_key;
CREATE UNIQUE INDEX subscriptions_rotation_week_key
    ON subscriptions (rotation_id, rotation_week)
    WHERE rotation_id IS NOT NULL AND status <> 'cancelled';

-- One row per swapped week: the unique key is what makes "at most one swap per
-- week" true, and a repeat of the same swap reads its result from here.
CREATE TABLE meal_rotation_swaps (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rotation_id         uuid NOT NULL REFERENCES meal_rotations(id) ON DELETE CASCADE,
    rotation_week       smallint NOT NULL CHECK (rotation_week BETWEEN 1 AND 4),
    old_subscription_id uuid NOT NULL REFERENCES subscriptions(id),
    new_subscription_id uuid NOT NULL REFERENCES subscriptions(id),
    old_amount_minor    bigint NOT NULL CHECK (old_amount_minor >= 0),
    new_amount_minor    bigint NOT NULL CHECK (new_amount_minor >= 0),
    currency            char(3) NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (rotation_id, rotation_week)
);
