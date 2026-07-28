-- Phase 7 — Logistics, the vendor console, and the admin surface.
--
-- PLAN.md §1 made a claim about the Fulfillment primitive and repeated it in the
-- comment above `CREATE TABLE fulfillments`:
--
--     Dispatch, routing and the driver app read this table and nothing else.
--     None of them ever learns what a laundry bag or a meal box is.
--
-- Six phases later, this is where that gets tested. Everything in §1 and §2
-- below joins to `fulfillments` and to nothing a vertical owns — no order_lines,
-- no menu_items, no subscription_meal_selections, no vehicle_listings. If a
-- column here had needed to know what was in the bag, the seam would have
-- leaked, and `internal/app/seam_test.go` now fails the build if the Go side
-- ever does.
--
-- 20 tables.

-- ─────────────────────────────────────────────────────────────────────────────
-- 1. Drivers
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE drivers (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,

    -- A driver belongs to the platform (NULL) or to one vendor.
    --
    -- PLAN.md §5 P4 and docs/README.md decision #5 leave "who delivers"
    -- unanswered — platform fleet, vendor's own, or 3PL. The nullable column is
    -- what lets the answer arrive later without a migration: a vendor-employed
    -- driver sees only that vendor's work, a platform driver sees a branch's
    -- work regardless of vendor, and the dispatch query is the same query.
    vendor_id   uuid REFERENCES vendors (id) ON DELETE CASCADE,

    display_name text NOT NULL,
    phone        text NOT NULL,
    vehicle_type text NOT NULL DEFAULT 'car'
                      CHECK (vehicle_type IN ('bike', 'motorcycle', 'car', 'van', 'truck')),
    plate_no     text,

    status       text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'active', 'suspended', 'offboarded')),

    -- Capacity, in fulfillments the driver will accept on one route. A bike
    -- rider carrying twelve meal boxes and a van carrying twelve are different
    -- propositions, and the router needs a number rather than a vehicle type.
    max_stops    smallint NOT NULL DEFAULT 20 CHECK (max_stops BETWEEN 1 AND 100),

    -- Denormalised last known position. The ping history lives in
    -- driver_location_pings; this column is what the dispatch board and the
    -- customer's tracking screen read, and it is one row per driver rather than
    -- a max() over a partitioned table on every poll.
    last_point   geography(Point, 4326),
    last_ping_at timestamptz,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

CREATE INDEX drivers_vendor_idx ON drivers (vendor_id) WHERE vendor_id IS NOT NULL;
CREATE INDEX drivers_active_idx ON drivers (status) WHERE status = 'active';
-- Nearest-driver lookup for live assignment.
CREATE INDEX drivers_point_idx ON drivers USING gist (last_point)
    WHERE status = 'active';

CREATE TRIGGER drivers_set_updated_at BEFORE UPDATE ON drivers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE driver_documents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id   uuid NOT NULL REFERENCES drivers (id) ON DELETE CASCADE,
    kind        text NOT NULL
                     CHECK (kind IN ('licence', 'emirates_id', 'vehicle_registration',
                                      'insurance', 'right_to_work')),
    -- Never the document itself, same rule as seller_verifications: a scan of an
    -- Emirates ID is the highest-value thing in this database to an attacker and
    -- has no business in Postgres.
    document_ref text,
    state       text NOT NULL DEFAULT 'pending'
                     CHECK (state IN ('pending', 'verified', 'rejected', 'expired')),
    expires_at  date,
    verified_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (driver_id, kind)
);

-- Drives the expiry sweeper. A driver whose insurance lapsed is a liability the
-- platform is carrying without knowing it.
CREATE INDEX driver_documents_expiry_idx ON driver_documents (expires_at)
    WHERE state = 'verified' AND expires_at IS NOT NULL;


-- A shift is the unit of availability.
--
-- Not a boolean "online" flag on the driver. A flag cannot answer "was anyone
-- on shift when this order came in", which is the first question asked after a
-- late delivery, and it cannot be planned against a week in advance.
CREATE TABLE driver_shifts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id   uuid NOT NULL REFERENCES drivers (id) ON DELETE CASCADE,
    branch_id   uuid REFERENCES vendor_branches (id) ON DELETE SET NULL,

    starts_at   timestamptz NOT NULL,
    ends_at     timestamptz NOT NULL,
    started_at  timestamptz,
    ended_at    timestamptz,

    status      text NOT NULL DEFAULT 'planned'
                     CHECK (status IN ('planned', 'active', 'ended', 'missed', 'cancelled')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT driver_shifts_window CHECK (ends_at > starts_at)
);

CREATE INDEX driver_shifts_driver_idx ON driver_shifts (driver_id, starts_at DESC);
CREATE INDEX driver_shifts_open_idx ON driver_shifts (starts_at)
    WHERE status IN ('planned', 'active');

CREATE TRIGGER driver_shifts_set_updated_at BEFORE UPDATE ON driver_shifts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Location history, partitioned by day.
--
-- The highest-volume table in the system after impressions: a driver pinging
-- every fifteen seconds for an eight-hour shift is ~1,900 rows, and fifty
-- drivers is 95,000 rows a day. Partitioned so retention is a DETACH rather than
-- a DELETE that vacuums for an hour — and it does need retention, because this
-- is location data about identifiable people and keeping it forever is a
-- liability, not an asset.
CREATE TABLE driver_location_pings (
    driver_id  uuid NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    point      geography(Point, 4326) NOT NULL,
    accuracy_m real,
    speed_kmh  real,
    heading    real,
    -- Which route this ping belongs to, when the driver is on one. Nullable: a
    -- driver on shift with no assigned work still reports position so dispatch
    -- can see who is where.
    route_id   uuid,
    battery_pct smallint
) PARTITION BY RANGE (recorded_at);

CREATE INDEX driver_location_pings_driver_idx
    ON driver_location_pings (driver_id, recorded_at DESC);

-- Rolling partitions, same helper shape as feed_impressions in 000013.
CREATE OR REPLACE FUNCTION ensure_driver_ping_partitions(p_days int DEFAULT 7)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    d date;
    part_name text;
BEGIN
    FOR i IN 0..p_days LOOP
        d := (current_date + i);
        part_name := format('driver_location_pings_%s', to_char(d, 'YYYYMMDD'));
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF driver_location_pings FOR VALUES FROM (%L) TO (%L)',
                part_name, d, d + 1);
        END IF;
    END LOOP;
END;
$$;

SELECT ensure_driver_ping_partitions(7);

CREATE TABLE driver_location_pings_default PARTITION OF driver_location_pings DEFAULT;


-- ─────────────────────────────────────────────────────────────────────────────
-- 2. Routes and assignment
-- ─────────────────────────────────────────────────────────────────────────────

-- A route is a driver's ordered work for part of a day.
--
-- Scoped to a branch and a date rather than "all of a driver's day", because
-- the thing being planned is a departure from one origin: the driver collects
-- from the kitchen, delivers N stops, returns. A driver serving two branches
-- has two routes, which is correct — they are two round trips.
CREATE TABLE delivery_routes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id   uuid REFERENCES drivers (id) ON DELETE SET NULL,
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    shift_id    uuid REFERENCES driver_shifts (id) ON DELETE SET NULL,

    service_date date NOT NULL,
    window_id   uuid REFERENCES time_windows (id),

    status      text NOT NULL DEFAULT 'planned'
                     CHECK (status IN ('planned', 'assigned', 'in_progress',
                                        'completed', 'abandoned', 'cancelled')),

    -- Planned figures come from the sequencer; actual figures are stamped from
    -- driver events. Keeping both is the only way to find out whether the
    -- routing is any good — a plan with no outturn to compare against is a
    -- number nobody can improve.
    planned_stops   smallint NOT NULL DEFAULT 0,
    planned_distance_m int,
    planned_duration_s int,
    actual_distance_m  int,

    started_at  timestamptz,
    completed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX delivery_routes_board_idx
    ON delivery_routes (branch_id, service_date, status);
CREATE INDEX delivery_routes_driver_idx
    ON delivery_routes (driver_id, service_date DESC)
    WHERE driver_id IS NOT NULL;

CREATE TRIGGER delivery_routes_set_updated_at BEFORE UPDATE ON delivery_routes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- The join between routing and the seam.
--
-- Note what this table holds: a fulfillment id, a position, and times. It does
-- not hold what is being delivered, what it is worth, or which vertical it came
-- from — the driver app renders the address, the window and the customer name,
-- all of which the fulfillment already snapshotted. That is the primitive
-- paying off.
CREATE TABLE route_stops (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    route_id    uuid NOT NULL REFERENCES delivery_routes (id) ON DELETE CASCADE,
    fulfillment_id uuid NOT NULL REFERENCES fulfillments (id) ON DELETE CASCADE,

    sequence    smallint NOT NULL CHECK (sequence > 0),
    eta         timestamptz,
    arrived_at  timestamptz,
    departed_at timestamptz,

    -- Distance and duration from the *previous* stop. Stored per leg rather than
    -- as a route total so a single bad leg is visible rather than averaged away.
    leg_distance_m int,
    leg_duration_s int,

    created_at  timestamptz NOT NULL DEFAULT now(),

    -- One position per route, and one route per fulfillment at a time. The
    -- second is what stops a fulfillment being live on two drivers' screens,
    -- which is how two drivers arrive at one door.
    UNIQUE (route_id, sequence),
    UNIQUE (route_id, fulfillment_id)
);

CREATE INDEX route_stops_fulfillment_idx ON route_stops (fulfillment_id);

-- A fulfillment may appear on only one *open* route. Enforced here rather than
-- in application code because the failure mode — a double dispatch — is
-- expensive, visible to the customer, and produced by exactly the kind of race
-- an application check does not see.
CREATE UNIQUE INDEX route_stops_one_open_route_idx
    ON route_stops (fulfillment_id)
    WHERE departed_at IS NULL;


-- Assignment history, append-only.
--
-- Separate from route_stops because a fulfillment can be reassigned — a driver
-- calls in sick, a bike breaks down — and "who was supposed to deliver this at
-- 14:00" must survive the reassignment. route_stops holds the current plan;
-- this holds what was ever true.
CREATE TABLE fulfillment_assignments (
    id          bigserial PRIMARY KEY,
    fulfillment_id uuid NOT NULL REFERENCES fulfillments (id) ON DELETE CASCADE,
    driver_id   uuid REFERENCES drivers (id) ON DELETE SET NULL,
    route_id    uuid REFERENCES delivery_routes (id) ON DELETE SET NULL,

    assigned_by text NOT NULL DEFAULT 'system'
                     CHECK (assigned_by IN ('system', 'dispatcher', 'vendor', 'self')),
    assigned_by_id uuid,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    release_reason text
);

CREATE INDEX fulfillment_assignments_fulfillment_idx
    ON fulfillment_assignments (fulfillment_id, assigned_at DESC);
CREATE INDEX fulfillment_assignments_driver_idx
    ON fulfillment_assignments (driver_id, assigned_at DESC)
    WHERE driver_id IS NOT NULL;


-- ─────────────────────────────────────────────────────────────────────────────
-- 3. Completion — proof and failure
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE proof_of_delivery (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    fulfillment_id uuid NOT NULL REFERENCES fulfillments (id) ON DELETE CASCADE,

    -- What kind of proof the driver captured. `otp` is the strong one and the
    -- one a dispute is won with: a code only the customer received, entered at
    -- the door. `left_at_door` is the honest name for the weakest — it is not
    -- proof of anything except that the driver says so.
    kind        text NOT NULL
                     CHECK (kind IN ('otp', 'signature', 'photo', 'recipient_name', 'left_at_door')),
    otp_verified boolean NOT NULL DEFAULT false,
    signature_key text,
    photo_key   text,
    recipient_name text,
    note        text,

    -- Where the driver was when they captured it, and how far that was from the
    -- fulfillment's own point. A proof captured 800 m from the address is the
    -- single most useful number in a "it never arrived" dispute.
    captured_point geography(Point, 4326),
    distance_m  int,

    captured_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (fulfillment_id)
);

CREATE INDEX proof_of_delivery_far_idx ON proof_of_delivery (distance_m DESC)
    WHERE distance_m IS NOT NULL;


-- Every attempt, not just the last one.
--
-- A fulfillment that failed twice and succeeded on the third try is a different
-- operational fact from one that succeeded first time, and a status column
-- records neither. This is also what a redelivery fee would be computed from, if
-- one is ever charged.
CREATE TABLE delivery_attempts (
    id          bigserial PRIMARY KEY,
    fulfillment_id uuid NOT NULL REFERENCES fulfillments (id) ON DELETE CASCADE,
    driver_id   uuid REFERENCES drivers (id) ON DELETE SET NULL,
    attempt_no  smallint NOT NULL CHECK (attempt_no > 0),

    outcome     text NOT NULL
                     CHECK (outcome IN ('delivered', 'customer_absent', 'wrong_address',
                                         'refused', 'unreachable', 'access_denied',
                                         'vehicle_issue', 'cancelled_by_customer')),
    note        text,
    point       geography(Point, 4326),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (fulfillment_id, attempt_no)
);

CREATE INDEX delivery_attempts_fulfillment_idx
    ON delivery_attempts (fulfillment_id, attempt_no);
-- Feeds the failure-reason report the ops console shows. `customer_absent`
-- dominating means the time windows are wrong; `wrong_address` dominating means
-- the address capture is.
CREATE INDEX delivery_attempts_outcome_idx ON delivery_attempts (outcome, occurred_at DESC);


-- ─────────────────────────────────────────────────────────────────────────────
-- 4. Cash on delivery
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Not in PLAN.md §5 P4's list, and included anyway. Two reasons.
--
-- First: `driver_float` has been in the ledger_account_type enum since migration
-- 000001 with nothing posting to it — the same situation `deferred_revenue` was
-- in before Phase 5, and it was put there for exactly this.
--
-- Second and more important: cash is the dominant payment method for delivery in
-- this region. A delivery product that cannot take cash is not a delivery
-- product here, and discovering that after the driver app ships means rebuilding
-- the completion flow with money in it.

CREATE TABLE cod_collections (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    fulfillment_id uuid NOT NULL REFERENCES fulfillments (id) ON DELETE RESTRICT,
    driver_id   uuid NOT NULL REFERENCES drivers (id) ON DELETE RESTRICT,
    order_id    uuid REFERENCES orders (id) ON DELETE RESTRICT,

    expected_minor bigint NOT NULL CHECK (expected_minor > 0),
    -- What the driver actually took. Allowed to differ from expected, because it
    -- does: the customer had no change, or paid part by card at the door. A
    -- schema that forbids the difference gets a driver entering a lie instead.
    collected_minor bigint NOT NULL CHECK (collected_minor >= 0),
    currency    char(3) NOT NULL DEFAULT 'AED',

    state       text NOT NULL DEFAULT 'held'
                     CHECK (state IN ('held', 'remitted', 'disputed', 'written_off')),
    remittance_id uuid,

    collected_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (fulfillment_id)
);

CREATE INDEX cod_collections_driver_held_idx ON cod_collections (driver_id)
    WHERE state = 'held';


-- A remittance is a driver handing cash in.
--
-- The reconciling document: it names a set of collections and a total, and the
-- ledger entry that clears the driver's float is posted against it. Without this
-- table the float account has a balance and no explanation of what is in it.
CREATE TABLE cod_remittances (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id   uuid NOT NULL REFERENCES drivers (id) ON DELETE RESTRICT,
    reference_no text NOT NULL,

    declared_minor bigint NOT NULL CHECK (declared_minor >= 0),
    counted_minor  bigint CHECK (counted_minor IS NULL OR counted_minor >= 0),
    currency    char(3) NOT NULL DEFAULT 'AED',

    -- A shortfall is a fact, not an accusation, and it has to be representable
    -- before anyone can decide what it means. Written off, recovered from the
    -- driver, or absorbed — all three are decisions someone makes *after* the
    -- number exists.
    variance_minor bigint NOT NULL DEFAULT 0,

    state       text NOT NULL DEFAULT 'declared'
                     CHECK (state IN ('declared', 'counted', 'settled', 'disputed')),
    method      text NOT NULL DEFAULT 'cash'
                     CHECK (method IN ('cash', 'bank_transfer', 'deduction')),
    received_by uuid REFERENCES users (id) ON DELETE SET NULL,

    declared_at timestamptz NOT NULL DEFAULT now(),
    settled_at  timestamptz,
    UNIQUE (reference_no)
);

CREATE INDEX cod_remittances_driver_idx ON cod_remittances (driver_id, declared_at DESC);
CREATE INDEX cod_remittances_open_idx ON cod_remittances (declared_at)
    WHERE state IN ('declared', 'counted');

ALTER TABLE cod_collections
    ADD CONSTRAINT cod_collections_remittance_fk
    FOREIGN KEY (remittance_id) REFERENCES cod_remittances (id) ON DELETE SET NULL;


-- ─────────────────────────────────────────────────────────────────────────────
-- 5. Payouts
-- ─────────────────────────────────────────────────────────────────────────────
--
-- A statement is a *derived* document. Every number on it is a sum over
-- ledger_entries for the period, and the statement exists so a vendor can be
-- shown a stable version of that sum — not so the sum can be stored instead of
-- computed. If the two ever disagree, the ledger is right.

CREATE TABLE payout_runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    period_start date NOT NULL,
    period_end   date NOT NULL,
    currency    char(3) NOT NULL DEFAULT 'AED',
    state       text NOT NULL DEFAULT 'draft'
                     CHECK (state IN ('draft', 'approved', 'paid', 'cancelled')),
    statement_count int NOT NULL DEFAULT 0,
    total_minor  bigint NOT NULL DEFAULT 0,
    approved_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    approved_at  timestamptz,
    paid_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payout_runs_period CHECK (period_end >= period_start),
    UNIQUE (period_start, period_end, currency)
);


CREATE TABLE payout_statements (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id      uuid REFERENCES payout_runs (id) ON DELETE SET NULL,
    vendor_id   uuid NOT NULL REFERENCES vendors (id) ON DELETE RESTRICT,
    reference_no text NOT NULL,

    period_start date NOT NULL,
    period_end   date NOT NULL,
    currency    char(3) NOT NULL DEFAULT 'AED',

    -- The four numbers a vendor argues about, kept as separate columns rather
    -- than derived at render time. gross - commission - vat + adjustments = net,
    -- and the CHECK says so, because a statement whose lines do not add up is
    -- the fastest way to lose a vendor's trust in every other number.
    gross_minor      bigint NOT NULL DEFAULT 0,
    commission_minor bigint NOT NULL DEFAULT 0,
    vat_minor        bigint NOT NULL DEFAULT 0,
    adjustments_minor bigint NOT NULL DEFAULT 0,
    net_minor        bigint NOT NULL DEFAULT 0,

    state       text NOT NULL DEFAULT 'draft'
                     CHECK (state IN ('draft', 'issued', 'paid', 'disputed', 'cancelled')),
    issued_at   timestamptz,
    paid_at     timestamptz,
    payment_ref text,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT payout_statements_balances
        CHECK (net_minor = gross_minor - commission_minor - vat_minor + adjustments_minor),
    CONSTRAINT payout_statements_period CHECK (period_end >= period_start),
    UNIQUE (reference_no),
    -- One statement per vendor per period. Two is how a vendor gets paid twice.
    UNIQUE (vendor_id, period_start, period_end, currency)
);

CREATE INDEX payout_statements_vendor_idx
    ON payout_statements (vendor_id, period_end DESC);
CREATE INDEX payout_statements_unpaid_idx ON payout_statements (period_end)
    WHERE state IN ('draft', 'issued');

CREATE TRIGGER payout_statements_set_updated_at BEFORE UPDATE ON payout_statements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- One line per underlying event, pointing back at the ledger journal.
--
-- The pointer is what makes a statement auditable: a vendor querying a line gets
-- the journal, and the journal gets the order. Without it, "why is this AED 43"
-- is answered by re-running the report and hoping it produces the same number.
CREATE TABLE payout_statement_lines (
    id          bigserial PRIMARY KEY,
    statement_id uuid NOT NULL REFERENCES payout_statements (id) ON DELETE CASCADE,
    journal_id  uuid REFERENCES ledger_journals (id) ON DELETE SET NULL,

    occurred_on date NOT NULL,
    kind        text NOT NULL
                     CHECK (kind IN ('order', 'subscription', 'refund', 'adjustment',
                                      'cod_offset', 'penalty')),
    -- Generic on purpose, exactly like ledger_journals.reference_type: the
    -- statement must not grow a foreign key per vertical.
    reference_type text,
    reference_id   uuid,
    description text,

    gross_minor      bigint NOT NULL DEFAULT 0,
    commission_minor bigint NOT NULL DEFAULT 0,
    vat_minor        bigint NOT NULL DEFAULT 0,
    net_minor        bigint NOT NULL DEFAULT 0
);

CREATE INDEX payout_statement_lines_statement_idx
    ON payout_statement_lines (statement_id, occurred_on);


-- ─────────────────────────────────────────────────────────────────────────────
-- 6. Disputes and the admin audit trail
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE disputes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_no text NOT NULL,

    -- Generic subject, same reason as everywhere else: an order, a fulfillment,
    -- a payout statement and a listing are all disputable and none of them
    -- should earn this table a foreign key.
    subject_type text NOT NULL
                      CHECK (subject_type IN ('order', 'fulfillment', 'subscription',
                                               'payout_statement', 'listing')),
    subject_id   uuid NOT NULL,

    raised_by_type text NOT NULL CHECK (raised_by_type IN ('customer', 'vendor', 'driver', 'admin')),
    raised_by_id   uuid REFERENCES users (id) ON DELETE SET NULL,

    kind        text NOT NULL
                     CHECK (kind IN ('not_delivered', 'damaged', 'wrong_item', 'late',
                                      'quality', 'charged_incorrectly', 'payout_amount',
                                      'driver_conduct', 'other')),
    state       text NOT NULL DEFAULT 'open'
                     CHECK (state IN ('open', 'investigating', 'awaiting_response',
                                       'resolved', 'rejected', 'withdrawn')),

    -- What was actually done about it, and to whom. Separated from the free-text
    -- resolution note because "how often do we refund" is a question ops asks
    -- weekly and a note cannot answer.
    resolution  text CHECK (resolution IS NULL OR resolution IN
                     ('refunded', 'partially_refunded', 'redelivered', 'credited',
                      'no_action', 'vendor_charged', 'driver_penalised')),
    resolution_amount_minor bigint,
    resolution_note text,

    assigned_to uuid REFERENCES users (id) ON DELETE SET NULL,
    -- The clock ops is measured against. Set on creation from the dispute kind;
    -- a dispute with no due time is one nobody is late on.
    due_at      timestamptz,
    resolved_by uuid REFERENCES users (id) ON DELETE SET NULL,
    resolved_at timestamptz,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (reference_no)
);

CREATE INDEX disputes_subject_idx ON disputes (subject_type, subject_id);
CREATE INDEX disputes_open_idx ON disputes (due_at NULLS LAST)
    WHERE state IN ('open', 'investigating', 'awaiting_response');
CREATE INDEX disputes_assignee_idx ON disputes (assigned_to, state)
    WHERE assigned_to IS NOT NULL;

CREATE TRIGGER disputes_set_updated_at BEFORE UPDATE ON disputes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE dispute_events (
    id          bigserial PRIMARY KEY,
    dispute_id  uuid NOT NULL REFERENCES disputes (id) ON DELETE CASCADE,
    actor_type  text NOT NULL CHECK (actor_type IN ('customer', 'vendor', 'driver', 'admin', 'system')),
    actor_id    uuid,
    kind        text NOT NULL CHECK (kind IN ('note', 'state_change', 'evidence', 'message')),
    body        text,
    attachment_key text,
    from_state  text,
    to_state    text,
    -- Internal notes are not shown to the customer or vendor. One table with a
    -- flag rather than two, because the ordering between internal and external
    -- events is the thread and splitting it makes the thread unreadable.
    is_internal boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dispute_events_dispute_idx ON dispute_events (dispute_id, created_at);


-- Every privileged action, append-only.
--
-- Refunds, suspensions, manual status overrides, fraud confirmations. This is
-- the table that answers "who cancelled that order and why", which is otherwise
-- answerable only by correlating application logs that have already rotated.
--
-- Deliberately not a generic event bus: it records *decisions taken by staff*,
-- which is a much smaller and much more auditable set than "things that
-- happened".
CREATE TABLE admin_actions (
    id          bigserial PRIMARY KEY,
    actor_id    uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    action      text NOT NULL,
    subject_type text NOT NULL,
    subject_id  uuid,
    -- Required, not optional. A refund with no stated reason is indistinguishable
    -- from an unauthorised one six months later.
    reason      text NOT NULL,
    before_state jsonb,
    after_state  jsonb,
    request_id  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT admin_actions_reason_nonempty CHECK (length(btrim(reason)) > 0)
);

CREATE INDEX admin_actions_actor_idx ON admin_actions (actor_id, created_at DESC);
CREATE INDEX admin_actions_subject_idx ON admin_actions (subject_type, subject_id, created_at DESC);


-- ─────────────────────────────────────────────────────────────────────────────
-- 7. Fulfillment additions
-- ─────────────────────────────────────────────────────────────────────────────
--
-- Three columns on the core table, and it is worth being explicit that none of
-- them describes *what* is being delivered — they describe the work.

ALTER TABLE fulfillments
    -- Denormalised current assignment. fulfillment_assignments holds the history
    -- and is the source of truth; this is what the dispatch board and the
    -- customer's tracking screen read without a lateral join per row.
    ADD COLUMN driver_id uuid REFERENCES drivers (id) ON DELETE SET NULL,
    ADD COLUMN route_id  uuid REFERENCES delivery_routes (id) ON DELETE SET NULL,
    -- Attempts made. Denormalised from delivery_attempts for the same reason:
    -- "show me everything that failed twice" is a dispatch-board filter.
    ADD COLUMN attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    -- The delivery OTP. Generated when the fulfillment is dispatched, shown to
    -- the customer, entered by the driver. Hashed rather than stored: a database
    -- reader who can see every OTP can mark every delivery complete.
    ADD COLUMN otp_hash text,
    ADD COLUMN otp_issued_at timestamptz;

CREATE INDEX fulfillments_driver_idx ON fulfillments (driver_id, scheduled_date)
    WHERE driver_id IS NOT NULL;
-- The unassigned queue: the dispatch board's left-hand column.
CREATE INDEX fulfillments_unassigned_idx
    ON fulfillments (branch_id, scheduled_date, window_id)
    WHERE driver_id IS NULL AND status = 'scheduled';


-- Reference-number generators, same alphabet as the rest: no 0, O, 1 or I,
-- because a support agent reads these over the phone.
CREATE OR REPLACE FUNCTION generate_reference(p_prefix text)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_alphabet text := '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';
    v_suffix   text := '';
    i int;
BEGIN
    FOR i IN 1..6 LOOP
        v_suffix := v_suffix || substr(v_alphabet, 1 + floor(random() * length(v_alphabet))::int, 1);
    END LOOP;
    RETURN p_prefix || '-' || to_char(now(), 'YYMM') || '-' || v_suffix;
END;
$$;


COMMENT ON TABLE route_stops IS
    'Routing joins to fulfillments and to nothing a vertical owns. That is the Fulfillment primitive paying off.';
COMMENT ON TABLE payout_statements IS
    'Derived from ledger_entries. If a statement and the ledger disagree, the ledger is right.';
COMMENT ON TABLE admin_actions IS
    'Staff decisions, append-only, reason required. Not a generic event bus.';
COMMENT ON COLUMN fulfillments.otp_hash IS
    'Hashed. A reader who can see every OTP can mark every delivery complete.';
