-- Releasing capacity from a *committed* reservation.
--
-- Found while testing the admin console's cancel action. `release_capacity`
-- only touches `held` reservations — which is correct for what it was written
-- for, the expiry sweeper reclaiming abandoned checkouts. But a reservation
-- commits the moment an order is confirmed, and from that point nothing in the
-- system could ever give the slot back.
--
-- So every cancelled order permanently burned its delivery slot. The bucket's
-- `consumed` count never came down, and a branch that took and cancelled ten
-- orders in a morning showed as full for the rest of the day with nothing on
-- the manifest. Invisible until a vendor asks why they stopped getting
-- bookings.
--
-- Deliberately a separate function rather than a new branch inside
-- `release_capacity`. That one is called by the sweeper on a timer, and giving
-- it the power to un-commit confirmed orders means a bug in the sweeper can
-- silently free slots that real customers are holding. These are different
-- authorities and they stay different functions.
CREATE OR REPLACE FUNCTION release_committed_capacity(p_reservation_id uuid, p_reason text)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    v_bucket_id uuid;
    v_units int;
BEGIN
    UPDATE capacity_reservations
       SET state = 'released', released_at = now()
     WHERE id = p_reservation_id
       AND state = 'committed'
    RETURNING bucket_id, units INTO v_bucket_id, v_units;

    IF NOT FOUND THEN
        RETURN false;
    END IF;

    -- `consumed`, not `reserved`. commit_capacity moved the units across when
    -- the order was confirmed, and decrementing the wrong counter would leave
    -- the bucket permanently over-full while reporting free space.
    UPDATE capacity_buckets
       SET consumed = greatest(consumed - v_units, 0)
     WHERE id = v_bucket_id;

    RETURN true;
END;
$$;

COMMENT ON FUNCTION release_committed_capacity(uuid, text) IS
    'Gives back a slot held by a confirmed order that was then cancelled. Not for '
    'the expiry sweeper — that is release_capacity, which only touches held rows.';
