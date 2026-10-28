-- Promos: staff-controlled discounts and trials for meal subscriptions and
-- rotations. A trial is a promo with trial_days: the first (and only) cycle is
-- shortened to that many days, priced pro rata, and does not renew.
CREATE TABLE promos (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Upper-case. NULL for a public offer that is applied from the storefront
    -- without typing anything.
    code               text CHECK (code ~ '^[A-Z0-9_-]{3,32}$'),
    name               text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    name_ar            text NOT NULL DEFAULT '',
    description        text NOT NULL DEFAULT '',
    description_ar     text NOT NULL DEFAULT '',

    discount_type      text NOT NULL CHECK (discount_type IN ('percent', 'fixed', 'none')),
    percent_bp         int NOT NULL DEFAULT 0 CHECK (percent_bp BETWEEN 0 AND 10000),
    amount_minor       bigint NOT NULL DEFAULT 0 CHECK (amount_minor >= 0),
    max_discount_minor bigint CHECK (max_discount_minor > 0),
    currency           char(3) NOT NULL DEFAULT 'AED',
    trial_days         int CHECK (trial_days BETWEEN 1 AND 14),

    applies_to         text NOT NULL DEFAULT 'subscriptions'
                       CHECK (applies_to IN ('subscriptions', 'rotations', 'both')),
    audience           text NOT NULL DEFAULT 'everyone'
                       CHECK (audience IN ('everyone', 'new_customers')),
    -- Share of the discount the kitchen absorbs; the platform funds the rest
    -- out of its commission. 0 = platform pays all of it.
    kitchen_share_bp   int NOT NULL DEFAULT 0 CHECK (kitchen_share_bp BETWEEN 0 AND 10000),

    usage_limit        int CHECK (usage_limit > 0),
    per_customer_limit int CHECK (per_customer_limit > 0),
    -- Reserved at checkout, released when an unpaid checkout is superseded.
    -- Incremented with a guarded UPDATE, never read-then-write.
    usage_count        int NOT NULL DEFAULT 0 CHECK (usage_count >= 0),

    is_public          boolean NOT NULL DEFAULT false,
    is_active          boolean NOT NULL DEFAULT true,
    starts_at          timestamptz,
    ends_at            timestamptz,

    created_by         uuid REFERENCES users (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz,

    CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at),
    CHECK (code IS NOT NULL OR is_public),
    CHECK (discount_type <> 'none' OR trial_days IS NOT NULL),
    CHECK (discount_type <> 'percent' OR percent_bp > 0),
    CHECK (discount_type <> 'fixed' OR amount_minor > 0),
    CHECK (trial_days IS NULL OR applies_to = 'subscriptions')
);

CREATE UNIQUE INDEX promos_code_key ON promos (code) WHERE deleted_at IS NULL AND code IS NOT NULL;
CREATE INDEX promos_public_idx ON promos (is_public) WHERE deleted_at IS NULL AND is_active;

-- Kitchens a promo is limited to. No rows = every kitchen.
CREATE TABLE promo_branches (
    promo_id  uuid NOT NULL REFERENCES promos (id) ON DELETE CASCADE,
    branch_id uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    PRIMARY KEY (promo_id, branch_id)
);

CREATE TABLE promo_redemptions (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    promo_id              uuid NOT NULL REFERENCES promos (id),
    user_id               uuid NOT NULL REFERENCES users (id),
    subscription_id       uuid REFERENCES subscriptions (id) ON DELETE CASCADE,
    rotation_id           uuid REFERENCES meal_rotations (id) ON DELETE CASCADE,
    discount_minor        bigint NOT NULL CHECK (discount_minor >= 0),
    platform_funded_minor bigint NOT NULL CHECK (platform_funded_minor >= 0),
    currency              char(3) NOT NULL DEFAULT 'AED',
    status                text NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'redeemed', 'released')),
    created_at            timestamptz NOT NULL DEFAULT now(),
    redeemed_at           timestamptz,
    released_at           timestamptz,
    CHECK (num_nonnulls(subscription_id, rotation_id) = 1),
    CHECK (platform_funded_minor <= discount_minor)
);

CREATE INDEX promo_redemptions_user_idx ON promo_redemptions (promo_id, user_id) WHERE status <> 'released';
CREATE INDEX promo_redemptions_sub_idx ON promo_redemptions (subscription_id) WHERE subscription_id IS NOT NULL;
CREATE INDEX promo_redemptions_rot_idx ON promo_redemptions (rotation_id) WHERE rotation_id IS NOT NULL;

-- The platform-funded part of a cycle's discount. The kitchen is paid as if
-- the customer had paid it, so recognition adds it back, proportionally to
-- deliveries, out of platform revenue.
ALTER TABLE subscription_cycles
    ADD COLUMN promo_subsidy_minor      bigint NOT NULL DEFAULT 0 CHECK (promo_subsidy_minor >= 0),
    ADD COLUMN recognised_subsidy_minor bigint NOT NULL DEFAULT 0 CHECK (recognised_subsidy_minor >= 0);
