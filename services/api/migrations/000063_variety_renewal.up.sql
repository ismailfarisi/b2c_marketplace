-- Anti-boredom at renewal: what the customer loves or is tired of, how
-- adventurous the next rotation's kitchens should be, and whether a renewal
-- swaps kitchens in at all.

-- A heart or a "not again" on a kitchen or a dish. One row per subject; the
-- sentiment flips in place.
CREATE TABLE meal_tastes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subject_kind text NOT NULL CHECK (subject_kind IN ('kitchen', 'dish')),
    subject_id   uuid NOT NULL,
    sentiment    text NOT NULL CHECK (sentiment IN ('favourite', 'avoid')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, subject_kind, subject_id)
);

CREATE INDEX meal_tastes_user_sentiment_idx ON meal_tastes (user_id, sentiment, subject_kind);

-- Per-customer meal preferences. familiar: new kitchens in cuisines already
-- eaten or hearted; surprise: cuisines not eaten in the last two cycles;
-- balanced: a mix. Absent row = balanced.
CREATE TABLE meal_preferences (
    user_id    uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    adventure  text NOT NULL DEFAULT 'balanced'
                   CHECK (adventure IN ('familiar', 'balanced', 'surprise')),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Whether renewing swaps in fresh kitchens from the proposal. Existing
-- rotations keep today's behaviour (same four kitchens); new rotations are
-- created with it on.
ALTER TABLE meal_rotations
    ADD COLUMN refresh_on_renew boolean NOT NULL DEFAULT false;
