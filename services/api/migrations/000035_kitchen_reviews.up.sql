-- Kitchen reviews: one per subscription, only from a customer who has had at
-- least one delivery from that kitchen, so every rating is from a paying
-- subscriber. Staff can hide a review; the kitchen can reply once.
CREATE TABLE kitchen_reviews (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users (id),
    subscription_id uuid NOT NULL UNIQUE REFERENCES subscriptions (id) ON DELETE CASCADE,
    rating          smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment         text NOT NULL DEFAULT '' CHECK (length(comment) <= 1000),
    status          text NOT NULL DEFAULT 'published' CHECK (status IN ('published', 'hidden')),
    hidden_reason   text,
    hidden_by       uuid REFERENCES users (id),
    reply           text CHECK (reply IS NULL OR length(reply) <= 1000),
    replied_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX kitchen_reviews_branch_idx ON kitchen_reviews (branch_id, created_at DESC) WHERE status = 'published';

-- Per-kitchen rating cache, recomputed in the transaction that changes a
-- review. The vendor-level columns stay for other verticals.
ALTER TABLE vendor_branches
    ADD COLUMN rating                 numeric(3,2) CHECK (rating IS NULL OR rating BETWEEN 1 AND 5),
    ADD COLUMN rating_count           int NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
    -- Food-safety verification (e.g. Dubai Municipality food establishment
    -- licence), checked by staff against the document.
    ADD COLUMN food_safety_ref        text,
    ADD COLUMN food_safety_expires_on date,
    ADD COLUMN food_safety_verified_at timestamptz,
    ADD COLUMN food_safety_verified_by uuid REFERENCES users (id);
