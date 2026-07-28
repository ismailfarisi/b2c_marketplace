-- Reverse of 000016. Dependency order, then the two engine edits.

DROP FUNCTION IF EXISTS generate_listing_reference();

-- Restore the Phase 2 body: vendor branches only, no user-authored coverage.
CREATE OR REPLACE FUNCTION resolve_reel_coverage(p_reel_id uuid)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    v_author_type text;
    v_author_id   uuid;
    v_coverage    geography;
    v_kind        text;
    v_origin      geography;
BEGIN
    SELECT author_type, author_id INTO v_author_type, v_author_id
      FROM reels WHERE id = p_reel_id;

    IF v_author_type IS NULL THEN
        RETURN false;
    END IF;

    IF v_author_type = 'vendor_branch' THEN
        SELECT b.service_area, b.point
          INTO v_coverage, v_origin
          FROM vendor_branches b
         WHERE b.id = v_author_id
           AND b.deleted_at IS NULL
           AND b.is_active;
        v_kind := 'service_area';
    END IF;

    IF v_coverage IS NULL THEN
        RETURN false;
    END IF;

    UPDATE reels
       SET coverage             = v_coverage,
           coverage_kind        = v_kind,
           origin_point         = v_origin,
           coverage_resolved_at = now()
     WHERE id = p_reel_id;

    RETURN true;
END;
$$;

-- Undo the payments widening. This deletes listing-fee payments rather than
-- orphaning them: with order_id back to NOT NULL there is no row shape that
-- could hold them, and leaving the migration to fail on the ALTER would be a
-- worse outcome than a down migration that says what it destroys.
DELETE FROM payments WHERE package_purchase_id IS NOT NULL;

DROP INDEX IF EXISTS payments_purchase_idx;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_exactly_one_subject;
ALTER TABLE payments DROP COLUMN IF EXISTS package_purchase_id;
ALTER TABLE payments ALTER COLUMN order_id SET NOT NULL;

DROP TABLE IF EXISTS dealer_subscriptions;
DROP TABLE IF EXISTS listing_promotions;
DROP TABLE IF EXISTS package_purchases;
DROP TABLE IF EXISTS listing_packages;

DROP TABLE IF EXISTS trust_scores;
DROP TABLE IF EXISTS fraud_signals;
DROP TABLE IF EXISTS inspection_reports;
DROP TABLE IF EXISTS vehicle_history_checks;
DROP TABLE IF EXISTS seller_verifications;

-- Leads and offers point *back* at conversations, so they have to go first.
--
-- The original order dropped conversations before them and failed with
-- "cannot drop table conversations because other objects depend on it". Nobody
-- noticed for three phases because no migration in this repository had ever
-- been executed until Phase 8 — a down migration that has never run is a
-- rollback plan nobody has tested, which is the same as not having one.
DROP TABLE IF EXISTS listing_favorites;
DROP TABLE IF EXISTS saved_search_alerts;
DROP TABLE IF EXISTS saved_searches;
DROP TABLE IF EXISTS listing_offers;
DROP TABLE IF EXISTS listing_leads;

DROP TABLE IF EXISTS message_attachments;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;

DROP TABLE IF EXISTS listing_price_history;
DROP TABLE IF EXISTS listing_status_history;
DROP TABLE IF EXISTS listing_photos;
DROP TABLE IF EXISTS vehicle_listings;

DROP TABLE IF EXISTS dealer_profiles;

DROP TABLE IF EXISTS vehicle_specs;
DROP TABLE IF EXISTS vehicle_trims;
DROP TABLE IF EXISTS vehicle_models;
DROP TABLE IF EXISTS vehicle_makes;
