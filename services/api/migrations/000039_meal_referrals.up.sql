-- Referrals: a customer shares a personal invite code. A friend who uses it on
-- their first subscription or rotation gets the friend discount; once the
-- friend's first meal is delivered, the referrer gets a single-use voucher.
-- Both are promos, so pricing, the platform subsidy and kitchen payouts follow
-- the promo path unchanged.

ALTER TABLE promos
    ADD COLUMN kind text NOT NULL DEFAULT 'standard'
        CHECK (kind IN ('standard', 'referral_friend', 'referral_reward')),
    -- A referral voucher can only be redeemed by the customer who earned it.
    ADD COLUMN owner_user_id uuid REFERENCES users (id),
    ADD CONSTRAINT promos_reward_owner CHECK (kind <> 'referral_reward' OR owner_user_id IS NOT NULL);

CREATE INDEX promos_owner_idx ON promos (owner_user_id) WHERE owner_user_id IS NOT NULL;

-- One row: the programme staff configure.
CREATE TABLE referral_program (
    id                  boolean PRIMARY KEY DEFAULT true CHECK (id),
    is_active           boolean NOT NULL DEFAULT false,
    friend_amount_minor bigint NOT NULL DEFAULT 5000 CHECK (friend_amount_minor > 0),
    reward_amount_minor bigint NOT NULL DEFAULT 5000 CHECK (reward_amount_minor > 0),
    currency            char(3) NOT NULL DEFAULT 'AED',
    -- Vouchers a referrer can earn per calendar month (Asia/Dubai).
    monthly_cap         int NOT NULL DEFAULT 5 CHECK (monthly_cap BETWEEN 1 AND 100),
    reward_valid_days   int NOT NULL DEFAULT 90 CHECK (reward_valid_days BETWEEN 7 AND 365),
    -- The promo friends receive; created the first time staff save the programme.
    friend_promo_id     uuid REFERENCES promos (id),
    updated_by          uuid REFERENCES users (id),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

INSERT INTO referral_program DEFAULT VALUES;

CREATE TABLE referral_codes (
    user_id    uuid PRIMARY KEY REFERENCES users (id),
    code       text NOT NULL UNIQUE CHECK (code ~ '^[A-Z0-9]{6,12}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE referrals (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    referrer_id     uuid NOT NULL REFERENCES users (id),
    friend_id       uuid NOT NULL REFERENCES users (id),
    -- The friend's first checkout. A pending row whose checkout is abandoned
    -- is deleted with the promo hold, so the friend can try again.
    subscription_id uuid REFERENCES subscriptions (id) ON DELETE CASCADE,
    rotation_id     uuid REFERENCES meal_rotations (id) ON DELETE CASCADE,
    status          text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'rewarded', 'rejected', 'void')),
    reason          text NOT NULL DEFAULT '',
    reward_promo_id uuid REFERENCES promos (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    settled_at      timestamptz,
    CHECK (referrer_id <> friend_id),
    CHECK (num_nonnulls(subscription_id, rotation_id) = 1),
    CHECK (status <> 'rewarded' OR reward_promo_id IS NOT NULL)
);

-- A customer is referred at most once.
CREATE UNIQUE INDEX referrals_friend_key ON referrals (friend_id);
CREATE INDEX referrals_referrer_idx ON referrals (referrer_id, created_at DESC);
CREATE INDEX referrals_pending_idx ON referrals (created_at) WHERE status = 'pending';
