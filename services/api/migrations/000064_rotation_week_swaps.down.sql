DROP TABLE meal_rotation_swaps;

DROP INDEX subscriptions_rotation_week_key;
CREATE UNIQUE INDEX subscriptions_rotation_week_key
    ON subscriptions (rotation_id, rotation_week)
    WHERE rotation_id IS NOT NULL;
