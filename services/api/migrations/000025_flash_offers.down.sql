DROP TABLE IF EXISTS flash_offer_claims;
DROP TABLE IF EXISTS flash_offers;

-- Back to the three types that existed before. Written out rather than left
-- alone: a down migration that leaves a widened CHECK behind would let a rolled
-- back deployment write rows the rolled back code cannot render.
ALTER TABLE reels DROP CONSTRAINT IF EXISTS reels_subject_type_check;
ALTER TABLE reels ADD CONSTRAINT reels_subject_type_check
    CHECK (subject_type = ANY (ARRAY['service_offering', 'meal_plan', 'vehicle_listing']));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_kind_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_kind_check
    CHECK (kind = ANY (ARRAY['book_service', 'subscribe_plan', 'inquire_listing',
                             'open_menu', 'follow', 'share', 'external']));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_target_type_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_target_type_check
    CHECK (target_type = ANY (ARRAY['service_offering', 'meal_plan', 'vehicle_listing',
                                    'vendor_branch', 'vendor', 'url']));
