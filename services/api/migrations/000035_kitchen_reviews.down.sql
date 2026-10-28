ALTER TABLE vendor_branches
    DROP COLUMN IF EXISTS food_safety_verified_by,
    DROP COLUMN IF EXISTS food_safety_verified_at,
    DROP COLUMN IF EXISTS food_safety_expires_on,
    DROP COLUMN IF EXISTS food_safety_ref,
    DROP COLUMN IF EXISTS rating_count,
    DROP COLUMN IF EXISTS rating;
DROP TABLE IF EXISTS kitchen_reviews;
