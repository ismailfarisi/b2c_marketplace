-- A swapped week leaves its cancelled subscription next to the live one; the
-- full index below would refuse that pair, so the swapped-out rows leave the
-- rotation first (they stay as plain cancelled subscriptions).
UPDATE subscriptions SET rotation_id = NULL, rotation_week = NULL
 WHERE id IN (SELECT old_subscription_id FROM meal_rotation_swaps);

DROP TABLE meal_rotation_swaps;

DROP INDEX subscriptions_rotation_week_key;
CREATE UNIQUE INDEX subscriptions_rotation_week_key
    ON subscriptions (rotation_id, rotation_week)
    WHERE rotation_id IS NOT NULL;
