-- Reusable window definitions. "08:00-10:00" is defined once per branch and
-- referenced by every bucket and fulfillment, so renaming or re-timing a window
-- does not require rewriting history.
CREATE TABLE time_windows (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    label       text NOT NULL,
    label_ar    text,
    starts_at   time NOT NULL,
    ends_at     time NOT NULL,
    sort_order  int NOT NULL DEFAULT 0,
    is_active   boolean NOT NULL DEFAULT true,

    CONSTRAINT time_windows_ordered CHECK (ends_at > starts_at),
    UNIQUE (branch_id, starts_at, ends_at)
);

CREATE INDEX time_windows_branch_idx ON time_windows (branch_id) WHERE is_active;


-- THE CONCURRENCY HOTSPOT (PLAN.md §2.3).
--
-- Key is (branch, resource, date, window). Branch — not vendor: a chain's three
-- facilities have independent throughput and must not contend for one counter.
--
-- Resource key is text and open-ended because what is scarce differs per
-- vertical: 'pickup_slot' for laundry, 'kitchen_covers' for meals, 'chair' for
-- a salon. Two verticals at one branch either share a pool or don't, and that
-- is a data decision rather than a code branch.
--
-- The CHECK is the real guarantee. Application-level "read remaining, then
-- decide" is a lost-update race under any concurrency at all; the constraint
-- makes overbooking impossible even if every caller is buggy.
CREATE TABLE capacity_buckets (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id    uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    resource_key text NOT NULL,
    bucket_date  date NOT NULL,
    window_id    uuid NOT NULL REFERENCES time_windows (id) ON DELETE CASCADE,

    capacity_limit int NOT NULL,
    consumed       int NOT NULL DEFAULT 0,
    -- Held by unconfirmed reservations. Available = limit - consumed - reserved.
    reserved       int NOT NULL DEFAULT 0,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT capacity_no_overbook CHECK (consumed + reserved <= capacity_limit),
    CONSTRAINT capacity_nonneg CHECK (consumed >= 0 AND reserved >= 0 AND capacity_limit >= 0),
    UNIQUE (branch_id, resource_key, bucket_date, window_id)
);

-- Availability lookups are (branch, resource, date range) — this index shape
-- serves them directly.
CREATE INDEX capacity_lookup_idx
    ON capacity_buckets (branch_id, resource_key, bucket_date)
    INCLUDE (window_id, capacity_limit, consumed, reserved);

CREATE TRIGGER capacity_buckets_set_updated_at BEFORE UPDATE ON capacity_buckets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- TTL reservations. A customer selecting a slot holds it while they enter card
-- details; abandoning checkout must return the slot without human intervention.
--
-- Without this, every abandoned cart permanently destroys inventory, and the
-- failure is invisible until a vendor asks why they are fully booked with no
-- orders.
CREATE TABLE capacity_reservations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id   uuid NOT NULL REFERENCES capacity_buckets (id) ON DELETE CASCADE,
    order_id    uuid,
    units       int NOT NULL CHECK (units > 0),
    state       text NOT NULL DEFAULT 'held'
                    CHECK (state IN ('held', 'committed', 'released', 'expired')),
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz
);

-- The sweeper job scans this: held reservations past expiry, oldest first.
CREATE INDEX capacity_reservations_expiry_idx
    ON capacity_reservations (expires_at)
    WHERE state = 'held';
CREATE INDEX capacity_reservations_order_idx
    ON capacity_reservations (order_id)
    WHERE order_id IS NOT NULL;


-- Reserve atomically.
--
-- The UPDATE ... WHERE takes a row lock and re-checks availability in the same
-- statement, so two concurrent callers for the last slot serialise: one wins,
-- one gets 0 rows and a clean 409. No SELECT-then-UPDATE window exists for a
-- race to live in.
CREATE OR REPLACE FUNCTION reserve_capacity(
    p_bucket_id uuid,
    p_order_id  uuid,
    p_units     int,
    p_ttl       interval DEFAULT '15 minutes'
) RETURNS uuid AS $$
DECLARE
    v_reservation_id uuid;
    v_updated int;
BEGIN
    UPDATE capacity_buckets
       SET reserved = reserved + p_units
     WHERE id = p_bucket_id
       AND consumed + reserved + p_units <= capacity_limit;

    GET DIAGNOSTICS v_updated = ROW_COUNT;
    IF v_updated = 0 THEN
        RETURN NULL;  -- caller maps this to 409 capacity exhausted
    END IF;

    INSERT INTO capacity_reservations (bucket_id, order_id, units, expires_at)
    VALUES (p_bucket_id, p_order_id, p_units, now() + p_ttl)
    RETURNING id INTO v_reservation_id;

    RETURN v_reservation_id;
END;
$$ LANGUAGE plpgsql;


-- Move a held reservation into consumed. Called when payment authorizes.
CREATE OR REPLACE FUNCTION commit_capacity(p_reservation_id uuid)
RETURNS boolean AS $$
DECLARE
    v_bucket_id uuid;
    v_units int;
BEGIN
    UPDATE capacity_reservations
       SET state = 'committed'
     WHERE id = p_reservation_id
       AND state = 'held'
    RETURNING bucket_id, units INTO v_bucket_id, v_units;

    IF NOT FOUND THEN
        RETURN false;
    END IF;

    UPDATE capacity_buckets
       SET reserved = reserved - v_units,
           consumed = consumed + v_units
     WHERE id = v_bucket_id;

    RETURN true;
END;
$$ LANGUAGE plpgsql;


-- Release a held reservation: explicit cancellation, or the TTL sweeper.
CREATE OR REPLACE FUNCTION release_capacity(p_reservation_id uuid, p_reason text)
RETURNS boolean AS $$
DECLARE
    v_bucket_id uuid;
    v_units int;
BEGIN
    UPDATE capacity_reservations
       SET state = CASE WHEN p_reason = 'expired' THEN 'expired' ELSE 'released' END,
           released_at = now()
     WHERE id = p_reservation_id
       AND state = 'held'
    RETURNING bucket_id, units INTO v_bucket_id, v_units;

    IF NOT FOUND THEN
        RETURN false;
    END IF;

    UPDATE capacity_buckets
       SET reserved = reserved - v_units
     WHERE id = v_bucket_id;

    RETURN true;
END;
$$ LANGUAGE plpgsql;
