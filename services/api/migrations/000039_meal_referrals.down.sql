DROP TABLE IF EXISTS referrals;
DROP TABLE IF EXISTS referral_codes;
DROP TABLE IF EXISTS referral_program;
DELETE FROM promo_redemptions WHERE promo_id IN (SELECT id FROM promos WHERE kind <> 'standard');
DELETE FROM promos WHERE kind <> 'standard';
DROP INDEX IF EXISTS promos_owner_idx;
ALTER TABLE promos
    DROP CONSTRAINT IF EXISTS promos_reward_owner,
    DROP COLUMN IF EXISTS owner_user_id,
    DROP COLUMN IF EXISTS kind;
