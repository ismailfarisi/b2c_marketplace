DROP INDEX IF EXISTS meal_rotations_renewed_from_key;
ALTER TABLE meal_rotations DROP COLUMN IF EXISTS renewed_from_id, DROP COLUMN IF EXISTS auto_renew;
DROP INDEX IF EXISTS subscriptions_renewed_from_key;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS renewed_from_id;
