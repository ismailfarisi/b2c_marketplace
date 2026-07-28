-- Queries are compiled by sqlc against migrations/. A typo in a column name is a
-- build failure here, not a runtime error on the order path.

-- name: GetAvailability :many
-- Only windows with remaining capacity. A window absent from the result is
-- full, closed, or outside the operating calendar — the client must not be able
-- to distinguish those cases, because "full" is commercially sensitive and
-- "closed" invites retry loops.
SELECT
    b.bucket_date,
    b.window_id,
    w.starts_at,
    w.ends_at,
    (b.capacity_limit - b.consumed - b.reserved)::int AS remaining
FROM capacity_buckets b
JOIN time_windows w ON w.id = b.window_id
WHERE b.branch_id = @branch_id
  AND b.resource_key = @resource_key
  AND b.bucket_date BETWEEN @from_date AND @to_date
  AND b.capacity_limit > b.consumed + b.reserved
  AND w.is_active
ORDER BY b.bucket_date, w.starts_at;

-- name: ReserveCapacity :one
-- Delegates to the SQL function so the availability re-check and the row lock
-- happen in one statement. A SELECT-then-UPDATE here would reintroduce the
-- lost-update race the function exists to close.
SELECT reserve_capacity(@bucket_id, @order_id, @units, @ttl::interval) AS reservation_id;

-- name: CommitCapacity :one
SELECT commit_capacity(@reservation_id) AS committed;

-- name: ReleaseCapacity :one
SELECT release_capacity(@reservation_id, @reason) AS released;

-- name: FindBucket :one
SELECT id, capacity_limit, consumed, reserved
FROM capacity_buckets
WHERE branch_id = @branch_id
  AND resource_key = @resource_key
  AND bucket_date = @bucket_date
  AND window_id = @window_id;
