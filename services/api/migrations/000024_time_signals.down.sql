-- Dropping the table drops both indexes with it.
--
-- Nothing else references it: the projection is written by verticals and read by
-- the feed, and both treat a missing row as "this subject has no time
-- dimension", which is the same state the product was in before 000024. A reel
-- whose overlay loses its countdown still plays.
DROP TABLE IF EXISTS reel_time_signals;
