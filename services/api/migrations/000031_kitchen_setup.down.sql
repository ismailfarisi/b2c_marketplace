DROP INDEX IF EXISTS subscription_plans_rotation_idx;

ALTER TABLE subscription_plans
    DROP CONSTRAINT IF EXISTS subscription_plans_rotation_needs_price,
    DROP CONSTRAINT IF EXISTS subscription_plans_rotation_priced,
    DROP CONSTRAINT IF EXISTS subscription_plans_weekdays_valid,
    DROP CONSTRAINT IF EXISTS subscription_plans_meal_slots_valid,
    DROP COLUMN IF EXISTS rotation_week_price_minor,
    DROP COLUMN IF EXISTS rotation_enabled,
    DROP COLUMN IF EXISTS delivery_weekdays,
    DROP COLUMN IF EXISTS meal_slots;

ALTER TABLE vendor_branches
    DROP COLUMN IF EXISTS cover_image_url,
    DROP COLUMN IF EXISTS dietary_tags,
    DROP COLUMN IF EXISTS cuisines,
    DROP COLUMN IF EXISTS kitchen_description_ar,
    DROP COLUMN IF EXISTS kitchen_description;
