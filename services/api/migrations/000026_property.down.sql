DROP TABLE IF EXISTS property_status_history;
DROP TABLE IF EXISTS property_listings;

ALTER TABLE reels DROP CONSTRAINT IF EXISTS reels_subject_type_check;
ALTER TABLE reels ADD CONSTRAINT reels_subject_type_check
    CHECK (subject_type = ANY (ARRAY['service_offering', 'meal_plan',
                                     'vehicle_listing', 'flash_offer']));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_kind_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_kind_check
    CHECK (kind = ANY (ARRAY['book_service', 'subscribe_plan', 'inquire_listing',
                             'claim_offer',
                             'open_menu', 'follow', 'share', 'external']));

ALTER TABLE reel_actions DROP CONSTRAINT IF EXISTS reel_actions_target_type_check;
ALTER TABLE reel_actions ADD CONSTRAINT reel_actions_target_type_check
    CHECK (target_type = ANY (ARRAY['service_offering', 'meal_plan', 'vehicle_listing',
                                    'flash_offer', 'vendor_branch', 'vendor', 'url']));
