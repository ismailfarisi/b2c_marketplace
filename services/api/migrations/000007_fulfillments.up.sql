-- THE CORE TABLE.
--
-- A fulfillment is the atomic unit of physical work: someone goes somewhere at
-- a time to do a thing. A laundry order produces two rows (pickup, delivery); a
-- meal-plan day produces one; a home visit produces one.
--
-- Dispatch, routing and the driver app read this table and nothing else. None
-- of them ever learns what a laundry bag or a meal box is — that is the entire
-- point, and it is what lets a fifth vertical ship without touching logistics.
CREATE TABLE fulfillments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    uuid REFERENCES orders (id) ON DELETE CASCADE,
    -- Nullable and mutually exclusive with order_id in Phase 5: a subscription
    -- generates fulfillments directly without an order per delivery day.
    subscription_id uuid,

    branch_id   uuid NOT NULL REFERENCES vendor_branches (id),
    kind        fulfillment_kind NOT NULL,
    status      fulfillment_status NOT NULL DEFAULT 'scheduled',
    sequence    int NOT NULL DEFAULT 1,

    scheduled_date date NOT NULL,
    window_id   uuid NOT NULL REFERENCES time_windows (id),
    reservation_id uuid REFERENCES capacity_reservations (id),

    -- Address is SNAPSHOTTED, not referenced. A customer editing their saved
    -- address must not silently redirect a driver already en route, and a
    -- deleted address must not orphan a delivery in progress.
    address_id      uuid REFERENCES addresses (id),
    address_line    text NOT NULL,
    address_landmark text,
    address_area    text,
    address_city    text,
    address_notes   text,
    recipient_name  text,
    recipient_phone text,
    point           geography(Point, 4326) NOT NULL,

    -- Opaque to logistics. The owning vertical resolves it; dispatch passes it
    -- through without interpreting it.
    payload_ref text,

    dispatched_at timestamptz,
    arrived_at    timestamptz,
    completed_at  timestamptz,
    failed_at     timestamptz,
    failure_reason text,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- Exactly one parent. A fulfillment belonging to both an order and a
    -- subscription, or to neither, is meaningless work nobody can bill for.
    CONSTRAINT fulfillments_one_parent CHECK (
        (order_id IS NOT NULL AND subscription_id IS NULL) OR
        (order_id IS NULL AND subscription_id IS NOT NULL)
    ),
    UNIQUE (order_id, sequence)
);

-- The dispatch board query: everything a branch must do on a given day, in
-- window order. This is the index the driver app and the ops console both hit.
CREATE INDEX fulfillments_dispatch_idx
    ON fulfillments (branch_id, scheduled_date, status)
    INCLUDE (window_id, kind, sequence);

CREATE INDEX fulfillments_order_idx ON fulfillments (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX fulfillments_subscription_idx ON fulfillments (subscription_id)
    WHERE subscription_id IS NOT NULL;
-- Route planning is geographic and scoped to one day's open work.
CREATE INDEX fulfillments_point_idx ON fulfillments USING gist (point)
    WHERE status IN ('scheduled', 'dispatched', 'en_route');

CREATE TRIGGER fulfillments_set_updated_at BEFORE UPDATE ON fulfillments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Every status transition, append-only. Disputes are resolved from this, and
-- "when did the driver actually arrive" is not answerable from a mutable status
-- column.
CREATE TABLE fulfillment_events (
    id             bigserial PRIMARY KEY,
    fulfillment_id uuid NOT NULL REFERENCES fulfillments (id) ON DELETE CASCADE,
    from_status    fulfillment_status,
    to_status      fulfillment_status NOT NULL,
    actor_type     text NOT NULL CHECK (actor_type IN ('system', 'driver', 'vendor', 'customer', 'admin')),
    actor_id       uuid,
    point          geography(Point, 4326),
    metadata       jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX fulfillment_events_fulfillment_idx
    ON fulfillment_events (fulfillment_id, occurred_at);
