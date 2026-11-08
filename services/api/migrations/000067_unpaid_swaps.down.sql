DELETE FROM notification_templates WHERE key = 'meals.swap_unpaid';
ALTER TABLE meal_rotation_swaps DROP COLUMN IF EXISTS voided_at;
