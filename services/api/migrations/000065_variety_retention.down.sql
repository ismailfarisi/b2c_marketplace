ALTER TABLE subscription_pauses DROP CONSTRAINT IF EXISTS subscription_pauses_no_overlap;
DROP TABLE IF EXISTS subscription_guest_week_dismissals;
DROP TABLE IF EXISTS subscription_guest_weeks;
DROP INDEX IF EXISTS meal_rotations_cancellation_code_idx;
DROP INDEX IF EXISTS subscriptions_cancellation_code_idx;
ALTER TABLE meal_rotations DROP COLUMN IF EXISTS cancellation_code;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS cancellation_code;
