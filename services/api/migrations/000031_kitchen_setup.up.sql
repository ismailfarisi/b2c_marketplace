-- Kitchen setup: what a meal vendor needs to sell plans on the marketplace
-- without a staff member editing rows for them.
--
-- Two additions, both on existing tables. A separate "kitchen" table would be a
-- second row to keep in step with the branch for every kitchen that exists, and
-- a branch already is the unit that cooks, delivers and gets paid.

-- ── the storefront card ─────────────────────────────────────────────────────
ALTER TABLE vendor_branches
    ADD COLUMN kitchen_description    text,
    ADD COLUMN kitchen_description_ar text,
    ADD COLUMN cuisines               text[] NOT NULL DEFAULT '{}',
    ADD COLUMN dietary_tags           text[] NOT NULL DEFAULT '{}',
    ADD COLUMN cover_image_url        text;

-- ── what a plan delivers, and whether it joins the rotation ────────────────
--
-- `meal_slots` and `delivery_weekdays` make the plan's shape explicit. Until
-- now a plan only said "five days, one meal" and the customer's schedule rules
-- carried the rest, which is enough to bill but not enough to show a weekly
-- menu or to decide whether two kitchens can share a rotation: a lunch plan and
-- a dinner plan are both "one meal a day".
--
-- The rotation price is the kitchen's own number for one week of this plan
-- inside a four-kitchen rotation. The customer pays the sum of the four; the
-- platform's commission is taken on each week as usual.
ALTER TABLE subscription_plans
    ADD COLUMN meal_slots        text[]     NOT NULL DEFAULT '{lunch}',
    ADD COLUMN delivery_weekdays smallint[] NOT NULL DEFAULT '{0,1,2,3,4}',
    ADD COLUMN rotation_enabled  boolean    NOT NULL DEFAULT false,
    ADD COLUMN rotation_week_price_minor bigint,
    ADD CONSTRAINT subscription_plans_meal_slots_valid CHECK (
        cardinality(meal_slots) BETWEEN 1 AND 6
        AND meal_slots <@ ARRAY['breakfast', 'lunch', 'dinner', 'snack', 'suhoor', 'iftar']::text[]),
    ADD CONSTRAINT subscription_plans_weekdays_valid CHECK (
        cardinality(delivery_weekdays) BETWEEN 1 AND 7
        AND delivery_weekdays <@ ARRAY[0, 1, 2, 3, 4, 5, 6]::smallint[]
        AND cardinality(delivery_weekdays) >= days_per_week),
    ADD CONSTRAINT subscription_plans_rotation_priced CHECK (
        rotation_week_price_minor IS NULL OR rotation_week_price_minor > 0),
    ADD CONSTRAINT subscription_plans_rotation_needs_price CHECK (
        NOT rotation_enabled OR rotation_week_price_minor IS NOT NULL);

CREATE INDEX subscription_plans_rotation_idx
    ON subscription_plans (service_type) WHERE rotation_enabled AND is_active;

