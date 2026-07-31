-- Flash offers: a vendor's time-boxed sale.
--
-- Named for what it is rather than when it happens. The design called these
-- "midnight offers" because that is the case everybody pictures — a kitchen
-- clearing stock at 23:00 — but the same mechanic is a 14:00–16:00 lull filler,
-- a Friday brunch window, or a two-hour flash sale announced this morning. A
-- table called `midnight_offers` would have to be renamed the first time a
-- restaurant used it at noon.
--
-- This is the fourth subject type and the first user of ModeWindow, which the
-- subject seam has carried since it was written: "this is true between two
-- times", counting up and filling, as distinct from a countdown that drains.

CREATE TABLE flash_offers (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id  uuid NOT NULL REFERENCES vendor_branches (id) ON DELETE CASCADE,

    name        text NOT NULL,
    name_ar     text,
    description text,

    -- The window, in the branch's own wall clock.
    --
    -- `time` and not `timestamptz`, because this is a recurring rule and not an
    -- occurrence: "every night from ten" is the fact the vendor is stating, and
    -- storing tonight's instant would need rewriting every day and would be
    -- wrong for an hour twice a year in any market that shifts its clocks. The
    -- zone comes from vendor_branches.timezone, the same source the meal-plan
    -- cutoff reads.
    starts_at_local time NOT NULL,
    ends_at_local   time NOT NULL,

    -- An end before a start means the window crosses midnight — 22:00 to 02:00
    -- is one four-hour window, not a twenty-hour one. Allowed deliberately, and
    -- it is exactly the case the original name was reaching for.
    --
    -- Equal is refused: a zero-length window is a reel with a bar that never
    -- moves and a CTA that is never live.
    CONSTRAINT flash_offers_window_nonzero CHECK (starts_at_local <> ends_at_local),

    -- Which days it runs. ISO 8601: 1 = Monday … 7 = Sunday, matching Postgres's
    -- `isodow` so the containment test is a direct comparison rather than an
    -- off-by-one waiting to happen. Empty means every day.
    days_of_week smallint[] NOT NULL DEFAULT '{}',
    CONSTRAINT flash_offers_days_valid CHECK (
        days_of_week <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[]
    ),

    -- Calendar bounds for a sale that is not standing. Both null is a permanent
    -- nightly offer; runs_until is what makes "this weekend only" expressible
    -- without a cron job to delete the row.
    runs_from  date,
    runs_until date,
    CONSTRAINT flash_offers_range_ordered CHECK (
        runs_from IS NULL OR runs_until IS NULL OR runs_until >= runs_from
    ),

    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    -- What it costs the rest of the time. Optional, and the whole point of the
    -- format when present: an offer with no reference price is just a price.
    original_price_minor bigint CHECK (original_price_minor IS NULL OR original_price_minor > 0),
    currency             char(3) NOT NULL DEFAULT 'AED',
    CONSTRAINT flash_offers_discount_real CHECK (
        original_price_minor IS NULL OR original_price_minor > price_minor
    ),

    -- Scarcity, when the vendor means it.
    --
    -- Nullable rather than defaulted to a big number: "20 left" and "no limit"
    -- are different claims, and a manufactured count is the fastest way to teach
    -- buyers that the number on the overlay is decoration.
    quantity  integer CHECK (quantity IS NULL OR quantity > 0),
    claimed   integer NOT NULL DEFAULT 0 CHECK (claimed >= 0),
    CONSTRAINT flash_offers_not_oversold CHECK (quantity IS NULL OR claimed <= quantity),

    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX flash_offers_branch_idx ON flash_offers (branch_id) WHERE is_active;
-- The projector's sweep: every live offer, so tonight's window can be resolved
-- into an instant pair.
CREATE INDEX flash_offers_live_idx ON flash_offers (runs_until)
    WHERE is_active;

CREATE TRIGGER flash_offers_set_updated_at
    BEFORE UPDATE ON flash_offers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Claims. One row per person per offer, which is what makes "2 left" mean
-- anything and what stops one account taking the lot.
CREATE TABLE flash_offer_claims (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    offer_id   uuid NOT NULL REFERENCES flash_offers (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The reel that produced it, when one did. This is the attribution the whole
    -- video bet is measured by for this vertical, and it is nullable because a
    -- claim from the vendor's own page is a real claim that no reel earned.
    source_reel_id uuid REFERENCES reels (id) ON DELETE SET NULL,
    -- Which occurrence of a recurring window this claim belongs to. Without it a
    -- nightly offer could only ever be claimed once per person, forever.
    window_date date NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (offer_id, user_id, window_date)
);

CREATE INDEX flash_offer_claims_offer_idx ON flash_offer_claims (offer_id, window_date);

-- The seam's two polymorphic CHECKs have to learn the new type. They are the one
-- place adding a subject is not purely additive Go, and that is deliberate: the
-- database refusing an unknown subject_type is what stops a typo in a handler
-- writing rows nothing can render.
ALTER TABLE reels DROP CONSTRAINT IF EXISTS reels_subject_type_check;
ALTER TABLE reels ADD CONSTRAINT reels_subject_type_check
    CHECK (subject_type = ANY (ARRAY[
        'service_offering', 'meal_plan', 'vehicle_listing', 'flash_offer'
    ]));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_kind_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_kind_check
    CHECK (kind = ANY (ARRAY[
        'book_service', 'subscribe_plan', 'inquire_listing',
        'claim_offer',
        'open_menu', 'follow', 'share', 'external'
    ]));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_target_type_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_target_type_check
    CHECK (target_type = ANY (ARRAY[
        'service_offering', 'meal_plan', 'vehicle_listing', 'flash_offer',
        'vendor_branch', 'vendor', 'url'
    ]));
