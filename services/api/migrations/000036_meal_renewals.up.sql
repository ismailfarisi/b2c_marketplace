-- Renewal: auto-renew for rotations, and the link from a renewal back to what
-- it renewed so a one-tap renew is not bought twice.
ALTER TABLE subscriptions
    ADD COLUMN renewed_from_id uuid REFERENCES subscriptions (id);

CREATE UNIQUE INDEX subscriptions_renewed_from_key ON subscriptions (renewed_from_id)
    WHERE renewed_from_id IS NOT NULL AND status <> 'cancelled';

ALTER TABLE meal_rotations
    ADD COLUMN auto_renew      boolean NOT NULL DEFAULT false,
    ADD COLUMN renewed_from_id uuid REFERENCES meal_rotations (id);

CREATE UNIQUE INDEX meal_rotations_renewed_from_key ON meal_rotations (renewed_from_id)
    WHERE renewed_from_id IS NOT NULL AND status <> 'cancelled';
