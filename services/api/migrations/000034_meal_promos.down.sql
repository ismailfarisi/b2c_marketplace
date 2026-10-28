ALTER TABLE subscription_cycles
    DROP COLUMN IF EXISTS recognised_subsidy_minor,
    DROP COLUMN IF EXISTS promo_subsidy_minor;
DROP TABLE IF EXISTS promo_redemptions;
DROP TABLE IF EXISTS promo_branches;
DROP TABLE IF EXISTS promos;
