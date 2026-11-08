-- Boredom-aware cancellation + guest weeks for single-kitchen subscribers.
--
-- The founder's own pain: on a meal subscription he got bored by week 3. When a
-- customer leaves we want to know *why* in a form we can count, and before they
-- leave we want to offer one way out of the boredom that keeps the kitchen model
-- intact: a guest week, which is an ordinary one-week subscription with another
-- kitchen funded by the wallet credit of a credited pause on the original.

ALTER TABLE subscriptions
    ADD COLUMN cancellation_code text
        CHECK (cancellation_code IN (
            'bored_of_food', 'too_expensive', 'food_quality',
            'delivery_problems', 'moving_away', 'other'));

ALTER TABLE meal_rotations
    ADD COLUMN cancellation_code text
        CHECK (cancellation_code IN (
            'bored_of_food', 'too_expensive', 'food_quality',
            'delivery_problems', 'moving_away', 'other'));

CREATE INDEX subscriptions_cancellation_code_idx
    ON subscriptions (cancellation_code) WHERE cancellation_code IS NOT NULL;
CREATE INDEX meal_rotations_cancellation_code_idx
    ON meal_rotations (cancellation_code) WHERE cancellation_code IS NOT NULL;

-- One guest week per original subscription per billing cycle. The pause on the
-- original carries the credit (credit_mode = 'wallet'); the guest subscription
-- is a normal weekly subscription whose own cycle pays its own kitchen.
CREATE TABLE subscription_guest_weeks (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id       uuid NOT NULL REFERENCES subscriptions (id),
    guest_subscription_id uuid NOT NULL UNIQUE REFERENCES subscriptions (id),
    cycle_id              uuid NOT NULL REFERENCES subscription_cycles (id),
    pause_id              uuid NOT NULL REFERENCES subscription_pauses (id),
    week_start            date NOT NULL,
    week_end              date NOT NULL,
    currency              char(3) NOT NULL DEFAULT 'AED',
    -- What the original cycle credited to the wallet for the paused week.
    credit_minor          bigint NOT NULL CHECK (credit_minor >= 0),
    -- The guest week's full price; the guest kitchen is paid on this.
    guest_amount_minor    bigint NOT NULL CHECK (guest_amount_minor >= 0),
    -- Split of guest_amount_minor: wallet part + card part (sandbox).
    wallet_minor          bigint NOT NULL CHECK (wallet_minor >= 0),
    due_minor             bigint NOT NULL CHECK (due_minor >= 0),
    wallet_posted_at      timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_guest_weeks_once_per_cycle UNIQUE (subscription_id, cycle_id),
    CONSTRAINT subscription_guest_weeks_split CHECK (wallet_minor + due_minor = guest_amount_minor),
    CONSTRAINT subscription_guest_weeks_dates CHECK (week_end >= week_start)
);

-- "Not now" on the guest-week offer, once per cycle.
CREATE TABLE subscription_guest_week_dismissals (
    subscription_id uuid NOT NULL REFERENCES subscriptions (id),
    cycle_id        uuid NOT NULL REFERENCES subscription_cycles (id),
    dismissed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subscription_id, cycle_id)
);

-- One credit per day: a guest week's pause and an ordinary pause must never
-- cover the same day of a subscription, or the week is credited twice. The
-- service checks first; this is the backstop for two requests racing.
-- btree_gist (migration 000001) lets the uuid ride in the GiST index.
ALTER TABLE subscription_pauses
    ADD CONSTRAINT subscription_pauses_no_overlap
    EXCLUDE USING gist (subscription_id WITH =, daterange(from_date, to_date, '[]') WITH &&)
    WHERE (state IN ('scheduled', 'active'));
