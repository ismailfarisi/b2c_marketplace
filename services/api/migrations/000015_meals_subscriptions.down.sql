-- Reverse of 000015. Dependents before dependencies.

DELETE FROM service_types WHERE key IN ('meals_subscription', 'meals_one_off');

DROP INDEX IF EXISTS fulfillments_subscription_slot_idx;
ALTER TABLE fulfillments DROP CONSTRAINT IF EXISTS fulfillments_subscription_fkey;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_source_subscription_fkey;

DROP FUNCTION IF EXISTS generate_subscription_no();

DROP TABLE IF EXISTS subscription_events;
DROP TABLE IF EXISTS subscription_meal_selections;
DROP FUNCTION IF EXISTS create_meal_selection_partition(date);
DROP TABLE IF EXISTS subscription_modifications;
DROP TABLE IF EXISTS subscription_skips;
DROP TABLE IF EXISTS subscription_pauses;
DROP TABLE IF EXISTS subscription_schedule_rules;
DROP TABLE IF EXISTS subscription_cycles;
DROP TABLE IF EXISTS subscriptions;

DROP TABLE IF EXISTS plan_tiers;
DROP TABLE IF EXISTS subscription_plans;
DROP TABLE IF EXISTS menu_day_items;
DROP TABLE IF EXISTS menu_cycle_days;
DROP TABLE IF EXISTS menu_cycles;
DROP TABLE IF EXISTS menu_items;

DROP TABLE IF EXISTS branch_calendar_days;
