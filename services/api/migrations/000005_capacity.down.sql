DROP FUNCTION IF EXISTS release_capacity(uuid, text);
DROP FUNCTION IF EXISTS commit_capacity(uuid);
DROP FUNCTION IF EXISTS reserve_capacity(uuid, uuid, int, interval);
DROP TABLE IF EXISTS capacity_reservations;
DROP TABLE IF EXISTS capacity_buckets;
DROP TABLE IF EXISTS time_windows;
