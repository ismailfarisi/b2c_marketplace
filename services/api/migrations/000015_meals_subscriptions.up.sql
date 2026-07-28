-- Phase 5: the food vertical — meal plans, subscriptions, the rolling scheduler.
--
-- PLAN.md §5 P2 calls this "the architecture's exam" and states the gate: if
-- adding meals requires changing commerce or scheduling internals rather than
-- adding strategies, stop and fix the abstraction.
--
-- The result of that exam is recorded in docs/PHASE-5-FOOD-AND-SUBSCRIPTIONS.md
-- §2, and it is worth stating here too because it shaped this file: **meals as
-- a vertical passed; recurrence turned out to be a missing axis.** A meal box
-- ordered for tomorrow fits the Phase 1 engine with zero engine changes. A meal
-- *subscription* does not, and not because meals are special — because nothing
-- in the engine modelled recurrence, and a weekly laundry pickup would have hit
-- exactly the same wall.
--
-- So the tables below split cleanly in two:
--
--   • meal catalogue — genuinely vertical-specific, invisible to the engine
--   • subscriptions  — engine tables, usable by any vertical, that happen to
--                      be needed first by meals


-- ── the calendar Phase 0 promised ────────────────────────────────────────────

-- branch_hours (migration 000003) carries a comment saying Ramadan and public
-- holidays override it "via a separate calendar table (Phase 5)". This is it.
--
-- PLAN.md §12 lists Ramadan as a first-class scheduling concept rather than an
-- edge case, and the reason is specific to this market: meal subscriptions
-- invert during it. Lunch and dinner become suhoor and iftar, menus change, and
-- kitchen capacity shifts to two narrow windows at fixed times relative to
-- sunset. A platform that models that as "the vendor closes early" produces a
-- month of undeliverable schedules in its most important month.
CREATE TABLE branch_calendar_days (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    calendar_date date NOT NULL,

    kind        text NOT NULL CHECK (kind IN ('closed', 'special_hours', 'ramadan')),
    opens_at    time,
    closes_at   time,
    note        text,
    note_ar     text,

    created_at  timestamptz NOT NULL DEFAULT now(),

    -- 'closed' carries no hours; the other two must.
    CONSTRAINT branch_calendar_hours_present CHECK (
        (kind = 'closed' AND opens_at IS NULL AND closes_at IS NULL)
        OR (kind <> 'closed' AND opens_at IS NOT NULL AND closes_at IS NOT NULL
            AND closes_at > opens_at)
    ),
    UNIQUE (branch_id, calendar_date)
);

-- The scheduler's lookup: "does this branch have an override on any date in the
-- next fourteen days".
CREATE INDEX branch_calendar_days_lookup_idx
    ON branch_calendar_days (branch_id, calendar_date);


-- ── meal catalogue ───────────────────────────────────────────────────────────

-- A dish. Nutrition and allergens are first-class columns rather than free text
-- because customers filter on them and because getting an allergen wrong is the
-- one catalogue error in this vertical that hurts somebody.
CREATE TABLE menu_items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,

    name        text NOT NULL,
    name_ar     text,
    description text,
    description_ar text,
    image_url   text,

    calories    int CHECK (calories IS NULL OR calories >= 0),
    protein_g   numeric(6,2),
    carbs_g     numeric(6,2),
    fat_g       numeric(6,2),

    -- Arrays rather than a join table. These are short, closed-ish vocabularies
    -- read on every menu render and written by one vendor at a time; a join
    -- table would add two queries to the hottest catalogue read to model a
    -- cardinality that never exceeds a handful.
    allergens   text[] NOT NULL DEFAULT '{}',
    -- veg, vegan, keto, high_protein, gluten_free…
    tags        text[] NOT NULL DEFAULT '{}',

    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX menu_items_branch_idx ON menu_items (branch_id) WHERE is_active;
CREATE INDEX menu_items_tags_idx ON menu_items USING gin (tags);
CREATE INDEX menu_items_allergens_idx ON menu_items USING gin (allergens);

CREATE TRIGGER menu_items_set_updated_at BEFORE UPDATE ON menu_items
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- A rotating N-week menu. Kitchens plan in cycles, not in dates: "week 2,
-- Tuesday, lunch" is the unit a chef thinks in, and it repeats.
--
-- Storing dated menus instead would mean a vendor re-entering their menu every
-- month forever, which is the single most reliable way to end up with an empty
-- catalogue by month three.
CREATE TABLE menu_cycles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id   uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    name        text NOT NULL,
    name_ar     text,
    weeks       smallint NOT NULL DEFAULT 1 CHECK (weeks BETWEEN 1 AND 8),
    -- The date the cycle's week 1 begins. Everything else is derived from this
    -- by modular arithmetic, so a cycle has no per-date rows and never expires.
    anchor_date date NOT NULL,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX menu_cycles_branch_idx ON menu_cycles (branch_id) WHERE is_active;

CREATE TRIGGER menu_cycles_set_updated_at BEFORE UPDATE ON menu_cycles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE menu_cycle_days (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_id    uuid NOT NULL REFERENCES menu_cycles (id) ON DELETE CASCADE,
    week_no     smallint NOT NULL CHECK (week_no >= 1),
    -- 0 = Sunday, matching branch_hours. The GCC working week starts Sunday;
    -- ISO's Monday-first assumption produces off-by-one bugs in this market and
    -- they surface as meals delivered on the wrong day.
    weekday     smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),

    UNIQUE (cycle_id, week_no, weekday)
);


-- Which dishes are available on which cycle day, in which slot.
--
-- portion_cap is per (cycle day, dish): a kitchen can make forty biryanis and
-- ninety salads on the same Tuesday. Without it the only expressible limit is a
-- total cover count, and the kitchen runs out of one dish while the platform
-- keeps selling it.
CREATE TABLE menu_day_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cycle_day_id  uuid NOT NULL REFERENCES menu_cycle_days (id) ON DELETE CASCADE,
    menu_item_id  uuid NOT NULL REFERENCES menu_items (id) ON DELETE CASCADE,

    meal_slot     text NOT NULL CHECK (meal_slot IN (
                      'breakfast', 'lunch', 'dinner', 'snack', 'suhoor', 'iftar')),
    portion_cap   int CHECK (portion_cap IS NULL OR portion_cap > 0),
    -- Chosen when the customer expresses no preference, and for plans that do
    -- not offer selection at all.
    is_default    boolean NOT NULL DEFAULT false,
    sort_order    int NOT NULL DEFAULT 0,

    UNIQUE (cycle_day_id, meal_slot, menu_item_id)
);

CREATE INDEX menu_day_items_lookup_idx ON menu_day_items (cycle_day_id, meal_slot);
-- Exactly one default per (cycle day, slot). Two defaults means the selection
-- a customer gets depends on which row the planner read first.
CREATE UNIQUE INDEX menu_day_items_one_default_idx
    ON menu_day_items (cycle_day_id, meal_slot) WHERE is_default;


-- The plan template: what a customer is buying.
--
-- Note what is NOT here: no dish, no date, no customer. A plan is a shape —
-- "five days a week, two meals a day, four weeks, prepaid" — and the same shape
-- serves a kitchen, a laundry with a weekly pickup, and a cleaner. The service
-- type is a column precisely so that stays true.
CREATE TABLE subscription_plans (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id    uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,
    service_type text NOT NULL REFERENCES service_types (key),
    menu_cycle_id uuid REFERENCES menu_cycles (id) ON DELETE SET NULL,

    name         text NOT NULL,
    name_ar      text,
    description  text,
    description_ar text,
    image_url    text,

    -- The billing period, in days. 28 rather than "1 month" deliberately:
    -- calendar months make a 5-day-a-week plan cost a different amount in
    -- February, and every customer notices.
    cycle_days   int NOT NULL CHECK (cycle_days BETWEEN 1 AND 365),
    days_per_week smallint NOT NULL CHECK (days_per_week BETWEEN 1 AND 7),
    meals_per_day smallint NOT NULL DEFAULT 1 CHECK (meals_per_day BETWEEN 1 AND 6),

    -- How many pause-days a customer may take per cycle. Zero means none.
    -- Travel is the single most common reason a GCC meal subscription is
    -- cancelled rather than paused, so this number is a retention lever and not
    -- an operational detail.
    allowed_pause_days smallint NOT NULL DEFAULT 0 CHECK (allowed_pause_days >= 0),

    -- Hours before a delivery after which it can no longer be skipped or
    -- changed. The kitchen has already bought the ingredients; a skip after
    -- cutoff is food waste the vendor pays for.
    cutoff_hours smallint NOT NULL DEFAULT 20 CHECK (cutoff_hours BETWEEN 0 AND 168),

    -- Prepaid is the GCC norm for meal plans: the full cycle is charged up
    -- front and revenue is recognised daily as meals are delivered. That is why
    -- deferred_revenue exists in the ledger_account_type enum from migration
    -- 000001 — this is the phase that finally posts to it.
    is_prepaid   boolean NOT NULL DEFAULT true,

    price_minor  bigint NOT NULL CHECK (price_minor >= 0),
    currency     char(3) NOT NULL DEFAULT 'AED',
    tax_rate_bp  int NOT NULL DEFAULT 500,

    is_active    boolean NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscription_plans_branch_idx ON subscription_plans (branch_id) WHERE is_active;

CREATE TRIGGER subscription_plans_set_updated_at BEFORE UPDATE ON subscription_plans
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- 5-day vs 6-day, small vs large portion. A tier overrides the plan's numbers
-- and carries its own absolute price rather than a delta.
--
-- Absolute, not a delta, because a delta plus a plan price is two numbers that
-- can drift apart, and the one that ends up on the customer's card is whichever
-- the code happened to add.
CREATE TABLE plan_tiers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id      uuid NOT NULL REFERENCES subscription_plans (id) ON DELETE CASCADE,
    key          text NOT NULL,
    name         text NOT NULL,
    name_ar      text,

    days_per_week smallint CHECK (days_per_week IS NULL OR days_per_week BETWEEN 1 AND 7),
    meals_per_day smallint CHECK (meals_per_day IS NULL OR meals_per_day BETWEEN 1 AND 6),
    portion_size  text CHECK (portion_size IS NULL OR portion_size IN ('small', 'regular', 'large')),

    price_minor  bigint NOT NULL CHECK (price_minor >= 0),
    sort_order   int NOT NULL DEFAULT 0,
    is_active    boolean NOT NULL DEFAULT true,

    UNIQUE (plan_id, key)
);

CREATE INDEX plan_tiers_plan_idx ON plan_tiers (plan_id) WHERE is_active;


-- ── subscriptions: engine, not vertical ──────────────────────────────────────

-- A recurring commitment. Nothing below this line mentions food.
--
-- status is the operational state and it is deliberately not the same thing as
-- the cycle's billing state. A subscription can be `active` with a `past_due`
-- cycle — that is precisely the dunning grace period, and collapsing the two
-- would mean a failed card instantly stops deliveries, which PLAN.md §6 rules
-- out: "Meal customers must know *before* the delivery doesn't arrive."
CREATE TABLE subscriptions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_no text NOT NULL,

    user_id      uuid NOT NULL REFERENCES users (id),
    branch_id    uuid NOT NULL REFERENCES vendor_branches (id),
    vendor_id    uuid NOT NULL REFERENCES vendors (id),
    plan_id      uuid NOT NULL REFERENCES subscription_plans (id),
    tier_id      uuid REFERENCES plan_tiers (id),
    service_type text NOT NULL REFERENCES service_types (key),

    status       text NOT NULL DEFAULT 'pending_payment' CHECK (status IN (
                     'pending_payment', 'active', 'paused',
                     'suspended', 'cancelled', 'completed')),

    start_date   date NOT NULL,
    -- Moves when a pause is credited by extension rather than to the wallet.
    -- Nullable for an open-ended auto-renewing subscription.
    end_date     date,
    renewal_mode text NOT NULL DEFAULT 'auto' CHECK (renewal_mode IN ('auto', 'manual', 'none')),

    -- The branch's timezone at signup, snapshotted. Materialising "tomorrow"
    -- from a UTC clock at 21:00 in Dubai produces the wrong date, and a vendor
    -- relocating must not retroactively shift a customer's delivery dates.
    timezone     text NOT NULL DEFAULT 'Asia/Dubai',

    -- Default delivery address, snapshotted onto each fulfillment at
    -- materialisation. Per-day overrides live on the schedule rules — office
    -- Sunday to Thursday, home on Friday is the normal case here, not an edge
    -- one.
    address_id   uuid NOT NULL REFERENCES addresses (id),

    -- How many pause-days have been consumed in the current cycle. Reset when a
    -- cycle rolls; carried here rather than computed so that the limit check is
    -- one read on a hot path.
    pause_days_used smallint NOT NULL DEFAULT 0 CHECK (pause_days_used >= 0),

    -- Date through which fulfillments have been created. The rolling scheduler
    -- is idempotent on its own, but this makes the nightly job's work
    -- proportional to what is new rather than to the whole window.
    materialised_through date,

    cancelled_at timestamptz,
    cancellation_reason text,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscriptions_dates_ordered CHECK (end_date IS NULL OR end_date >= start_date)
);

CREATE UNIQUE INDEX subscriptions_no_key ON subscriptions (subscription_no);
CREATE INDEX subscriptions_user_idx ON subscriptions (user_id, created_at DESC);
CREATE INDEX subscriptions_branch_idx ON subscriptions (branch_id, status);
-- The nightly scheduler's scan: live subscriptions whose materialised horizon
-- has fallen inside the rolling window.
CREATE INDEX subscriptions_materialise_idx
    ON subscriptions (materialised_through NULLS FIRST)
    WHERE status IN ('active', 'paused');

CREATE TRIGGER subscriptions_set_updated_at BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- The billing period. One payment per cycle for a prepaid plan.
--
-- order_id points at a real row in `orders`, so a subscription charge reaches
-- payments, quotes, the ledger and the vendor's payout statement through the
-- same path as any other money in the system. A parallel billing table would
-- mean two ways for money to exist and two places to fix a reconciliation bug.
CREATE TABLE subscription_cycles (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    cycle_no     int NOT NULL CHECK (cycle_no >= 1),

    starts_on    date NOT NULL,
    ends_on      date NOT NULL,

    order_id     uuid REFERENCES orders (id),

    status       text NOT NULL DEFAULT 'pending' CHECK (status IN (
                     'pending', 'paid', 'past_due', 'cancelled', 'refunded')),

    amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
    currency     char(3) NOT NULL DEFAULT 'AED',

    -- Prepaid revenue is deferred at charge and recognised as meals are
    -- delivered. This column is the running recognised total; the ledger is the
    -- truth and this is the cheap read that tells the recogniser where it got
    -- to. They are reconciled by the daily job, and a mismatch is a bug worth
    -- alerting on rather than silently correcting.
    recognised_minor bigint NOT NULL DEFAULT 0 CHECK (recognised_minor >= 0),
    -- Delivery days in this cycle, fixed when the cycle opens. The daily
    -- recognition amount is amount/deliverable_days, and freezing the divisor
    -- means a mid-cycle pause does not retroactively change what was already
    -- recognised.
    deliverable_days int NOT NULL DEFAULT 0 CHECK (deliverable_days >= 0),

    -- Dunning. PLAN.md §6: failed renewal → retry schedule → grace period →
    -- suspend fulfillments. Never delete the subscription.
    attempts     smallint NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error   text,
    next_attempt_at timestamptz,
    grace_until  date,

    paid_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_cycles_dates_ordered CHECK (ends_on >= starts_on),
    CONSTRAINT subscription_cycles_recognised_bounded CHECK (recognised_minor <= amount_minor),
    UNIQUE (subscription_id, cycle_no)
);

CREATE INDEX subscription_cycles_subscription_idx
    ON subscription_cycles (subscription_id, starts_on DESC);
-- The dunning sweeper.
CREATE INDEX subscription_cycles_dunning_idx ON subscription_cycles (next_attempt_at)
    WHERE status = 'past_due';
-- The renewal sweeper: cycles ending soon that have no successor.
CREATE INDEX subscription_cycles_ends_idx ON subscription_cycles (ends_on)
    WHERE status IN ('paid', 'past_due');

CREATE TRIGGER subscription_cycles_set_updated_at BEFORE UPDATE ON subscription_cycles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- When deliveries happen, and where.
--
-- One row per (weekday, meal slot). The per-day address override is the reason
-- this is a table rather than three columns on `subscriptions`: office Sunday
-- to Thursday and home on Friday is the normal case in this market, and a
-- single delivery address forces the customer to choose which half of their
-- week to get wrong.
CREATE TABLE subscription_schedule_rules (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,

    weekday      smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    meal_slot    text NOT NULL CHECK (meal_slot IN (
                     'breakfast', 'lunch', 'dinner', 'snack', 'suhoor', 'iftar')),
    window_id    uuid NOT NULL REFERENCES time_windows (id),
    -- NULL falls back to subscriptions.address_id.
    address_id   uuid REFERENCES addresses (id),

    created_at   timestamptz NOT NULL DEFAULT now(),

    UNIQUE (subscription_id, weekday, meal_slot)
);

CREATE INDEX subscription_schedule_rules_subscription_idx
    ON subscription_schedule_rules (subscription_id);


-- A date range the customer is away.
--
-- Distinct from a skip, and the difference is not cosmetic: a pause consumes
-- the plan's pause allowance, may extend the end date or issue wallet credit,
-- and is a retention event. A skip is one meal the customer did not want.
CREATE TABLE subscription_pauses (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,

    from_date    date NOT NULL,
    to_date      date NOT NULL,

    -- extend: push end_date out by the paused delivery days.
    -- wallet:  credit the customer's wallet for them.
    -- none:    the customer forfeits, which is only ever set by an admin
    --          resolving a dispute — never by the customer-facing path.
    credit_mode  text NOT NULL DEFAULT 'extend'
                     CHECK (credit_mode IN ('extend', 'wallet', 'none')),

    -- Delivery days actually suppressed, computed at creation. Stored because
    -- the credit is derived from it and the schedule rules may change later.
    suppressed_days int NOT NULL DEFAULT 0 CHECK (suppressed_days >= 0),
    credit_minor int NOT NULL DEFAULT 0 CHECK (credit_minor >= 0),

    state        text NOT NULL DEFAULT 'scheduled'
                     CHECK (state IN ('scheduled', 'active', 'completed', 'cancelled')),

    created_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT subscription_pauses_dates_ordered CHECK (to_date >= from_date)
);

CREATE INDEX subscription_pauses_subscription_idx
    ON subscription_pauses (subscription_id, from_date)
    WHERE state IN ('scheduled', 'active');


CREATE TABLE subscription_skips (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    skip_date    date NOT NULL,
    -- NULL skips every slot on that date.
    meal_slot    text CHECK (meal_slot IS NULL OR meal_slot IN (
                     'breakfast', 'lunch', 'dinner', 'snack', 'suhoor', 'iftar')),
    reason       text,
    created_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- A slot-specific skip and a whole-day skip can coexist; the whole-day one
-- wins. Two identical rows cannot, which is what stops a double-tap from
-- looking like two skips against the allowance.
CREATE UNIQUE INDEX subscription_skips_unique_idx
    ON subscription_skips (subscription_id, skip_date, COALESCE(meal_slot, '*'));


-- Mid-cycle plan or tier change, with its proration record.
--
-- A change is a new row, never an UPDATE of the subscription's plan_id alone.
-- The old plan is what the customer was charged for the days already delivered,
-- and losing that makes the cycle's arithmetic unreconstructable.
CREATE TABLE subscription_modifications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    cycle_id     uuid REFERENCES subscription_cycles (id) ON DELETE SET NULL,

    from_plan_id uuid REFERENCES subscription_plans (id),
    from_tier_id uuid REFERENCES plan_tiers (id),
    to_plan_id   uuid NOT NULL REFERENCES subscription_plans (id),
    to_tier_id   uuid REFERENCES plan_tiers (id),

    effective_from date NOT NULL,
    -- Positive means the customer owes more; negative means a credit. Signed
    -- rather than split into two columns because it is one number in one
    -- direction and two columns invite one of them being forgotten.
    proration_minor bigint NOT NULL DEFAULT 0,
    currency     char(3) NOT NULL DEFAULT 'AED',

    created_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscription_modifications_subscription_idx
    ON subscription_modifications (subscription_id, effective_from);


-- Which dish, on which day, in which slot.
--
-- DATA-MODEL.md flags this as the highest-volume table in the system, and the
-- arithmetic backs it up: one row per customer per meal per day. Ten thousand
-- subscribers on a two-meal plan is 600,000 rows a month, growing linearly and
-- forever, and every one of them is read exactly twice — once by the kitchen's
-- prep sheet and once by the customer.
--
-- Range-partitioned by service_date from the start, for the same reason
-- feed_impressions was: the alternative is discovering it at 400 million rows.
CREATE TABLE subscription_meal_selections (
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    service_date date NOT NULL,
    meal_slot    text NOT NULL CHECK (meal_slot IN (
                     'breakfast', 'lunch', 'dinner', 'snack', 'suhoor', 'iftar')),

    menu_item_id uuid REFERENCES menu_items (id) ON DELETE SET NULL,
    -- Snapshot, for the same reason order_lines snapshots price and name: a
    -- vendor renaming a dish must not rewrite what a customer was served last
    -- month, and a deleted dish must not make history unreadable.
    item_name_snapshot text,
    item_name_ar_snapshot text,

    -- Set once the day's fulfillment exists. Nullable because a selection can
    -- be made before the rolling window reaches that date.
    fulfillment_id uuid REFERENCES fulfillments (id) ON DELETE SET NULL,

    -- customer: they chose. default: the plan's default was applied because
    -- they did not choose before cutoff.
    chosen_by    text NOT NULL DEFAULT 'default'
                     CHECK (chosen_by IN ('customer', 'default', 'vendor')),

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- The partition key must be part of the primary key. It is anyway: the
    -- natural key of a selection is exactly (who, when, which meal).
    PRIMARY KEY (subscription_id, service_date, meal_slot)
) PARTITION BY RANGE (service_date);

-- The kitchen's prep sheet: "everything I am cooking on Tuesday, by dish".
CREATE INDEX subscription_meal_selections_day_idx
    ON subscription_meal_selections (service_date, menu_item_id);

-- Creates one month's partition, idempotently. Same shape and same reasoning as
-- create_feed_impression_partition: rows that land in DEFAULT block the later
-- creation of any partition whose range would have covered them.
CREATE OR REPLACE FUNCTION create_meal_selection_partition(p_month date)
RETURNS text
LANGUAGE plpgsql
AS $$
DECLARE
    v_start date := date_trunc('month', p_month)::date;
    v_end   date := (date_trunc('month', p_month) + interval '1 month')::date;
    v_name  text := 'subscription_meal_selections_' || to_char(v_start, 'YYYYMM');
BEGIN
    IF to_regclass(v_name) IS NOT NULL THEN
        RETURN v_name;
    END IF;

    EXECUTE format(
        'CREATE TABLE %I PARTITION OF subscription_meal_selections FOR VALUES FROM (%L) TO (%L)',
        v_name, v_start, v_end);

    RETURN v_name;
END;
$$;

-- The current month and the next two. Selections are made ahead of the rolling
-- fulfillment window, so this needs more headroom than impressions do.
SELECT create_meal_selection_partition(CURRENT_DATE);
SELECT create_meal_selection_partition((CURRENT_DATE + interval '1 month')::date);
SELECT create_meal_selection_partition((CURRENT_DATE + interval '2 months')::date);

CREATE TABLE subscription_meal_selections_default
    PARTITION OF subscription_meal_selections DEFAULT;


-- Every state transition, append-only. Support answers "why did my meals stop"
-- from this table, and a mutable status column cannot answer it.
CREATE TABLE subscription_events (
    id           bigserial PRIMARY KEY,
    subscription_id uuid NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    event_type   text NOT NULL,
    from_status  text,
    to_status    text,
    actor_type   text NOT NULL DEFAULT 'system'
                     CHECK (actor_type IN ('system', 'customer', 'vendor', 'admin')),
    actor_id     uuid,
    metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscription_events_subscription_idx
    ON subscription_events (subscription_id, occurred_at DESC);


-- ── close the loops Phase 1 left open ────────────────────────────────────────

-- Both columns were created in Phase 1 without constraints, deliberately, so
-- that adding subscriptions would not require a migration on tables that are
-- large by then. This is where they get their foreign keys.
ALTER TABLE orders
    ADD CONSTRAINT orders_source_subscription_fkey
    FOREIGN KEY (source_subscription_id) REFERENCES subscriptions (id) ON DELETE SET NULL;

ALTER TABLE fulfillments
    ADD CONSTRAINT fulfillments_subscription_fkey
    FOREIGN KEY (subscription_id) REFERENCES subscriptions (id) ON DELETE CASCADE;

-- The rolling scheduler must be safely re-runnable: the nightly job, a manual
-- trigger, and a retry after a partial failure all call it. This index makes
-- "already materialised" a lookup rather than a scan, and the uniqueness makes
-- double-materialisation impossible rather than merely unlikely.
--
-- Keyed on `sequence` rather than on anything meal-shaped. Sequence already
-- means "ordering within the parent", and for a subscription the parent is the
-- day — so slot 1 and slot 2 of a two-meal plan are sequences 1 and 2. Putting
-- a meal slot in this index would be the first place the word "meal" leaked
-- into a table that dispatch reads.
CREATE UNIQUE INDEX fulfillments_subscription_slot_idx
    ON fulfillments (subscription_id, scheduled_date, sequence)
    WHERE subscription_id IS NOT NULL;


-- The subscription number generator, mirroring generate_order_no. Support reads
-- these over the phone too.
CREATE OR REPLACE FUNCTION generate_subscription_no()
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
    RETURN 'SUB-' || to_char(now(), 'YYMM') || '-' || v_suffix;
END;
$$;


-- ── register the vertical ────────────────────────────────────────────────────

-- The code half of these rows is internal/verticals/meals. Two rows, not one:
-- a subscription and a one-off meal box are different fulfillment shapes and
-- different capacity draws, and the registry exists so that difference is data.
INSERT INTO service_types (
    key, name, name_ar, item_selection, fulfillment_pattern,
    capacity_resource, pricing_strategy, price_final_at_booking)
VALUES
    ('meals_subscription', 'Meal plan', 'خطة وجبات',
     'meals_subscription', 'recurring_delivery', 'meals_subscription', 'meals_subscription', true),
    ('meals_one_off', 'Meal box', 'صندوق وجبة',
     'meals_one_off', 'single_delivery', 'meals_one_off', 'meals_one_off', true)
ON CONFLICT (key) DO NOTHING;
