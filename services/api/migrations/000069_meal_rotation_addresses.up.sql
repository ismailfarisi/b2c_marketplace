-- Per-meal delivery addresses for a rotation: lunch at the office, dinner at
-- home. meal_rotations.address_id stays the default; a row here overrides it
-- for one meal slot across every week and every kitchen of the rotation. The
-- weekly subscriptions carry the same choice in their schedule rules, so this
-- table is what lets a renewal, swap or suggestion rebuild them.
CREATE TABLE meal_rotation_addresses (
    rotation_id uuid NOT NULL REFERENCES meal_rotations (id) ON DELETE CASCADE,
    meal_slot   text NOT NULL CHECK (meal_slot IN (
                    'breakfast', 'lunch', 'dinner', 'snack', 'suhoor', 'iftar')),
    address_id  uuid NOT NULL REFERENCES addresses (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (rotation_id, meal_slot)
);
