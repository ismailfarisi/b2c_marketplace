DROP INDEX IF EXISTS payments_provider_ref_key;
CREATE UNIQUE INDEX payments_provider_ref_key ON payments (provider, provider_ref)
    WHERE provider_ref IS NOT NULL;
DROP INDEX IF EXISTS subscriptions_rotation_branch_idx;
DROP INDEX IF EXISTS subscriptions_rotation_week_key;
ALTER TABLE subscriptions
    DROP CONSTRAINT IF EXISTS subscriptions_rotation_week_valid,
    DROP COLUMN IF EXISTS rotation_week,
    DROP COLUMN IF EXISTS rotation_id;
DROP FUNCTION IF EXISTS generate_rotation_no();
DROP TABLE IF EXISTS meal_rotations;
