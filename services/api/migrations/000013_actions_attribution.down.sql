DROP FUNCTION IF EXISTS create_feed_impression_partition(date);

DROP TABLE IF EXISTS reel_funnel_daily;
-- Dropping the parent drops every partition with it.
DROP TABLE IF EXISTS feed_impressions;
DROP TABLE IF EXISTS reel_saves;
DROP TABLE IF EXISTS follows;
DROP TABLE IF EXISTS order_attributions;
DROP TABLE IF EXISTS action_clicks;
DROP TABLE IF EXISTS reel_actions;
