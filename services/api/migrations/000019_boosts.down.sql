-- Reverse of 000019.

DROP INDEX IF EXISTS payments_campaign_idx;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_exactly_one_subject;
ALTER TABLE payments DROP COLUMN IF EXISTS campaign_id;
-- Restore the Phase 6 shape.
ALTER TABLE payments ADD CONSTRAINT payments_exactly_one_subject
    CHECK (num_nonnulls(order_id, package_purchase_id) = 1);

ALTER TABLE boost_deliveries DROP CONSTRAINT IF EXISTS boost_deliveries_charge_fk;
DROP TABLE IF EXISTS boost_charges;

DROP FUNCTION IF EXISTS create_boost_delivery_partition(date);
-- Partitions go with the parent.
DROP TABLE IF EXISTS boost_deliveries;

DROP TABLE IF EXISTS boost_daily_spend;
DROP TABLE IF EXISTS boost_targeting;
DROP TABLE IF EXISTS boost_campaigns;
DROP TABLE IF EXISTS boost_rate_cards;
