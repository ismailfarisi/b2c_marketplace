DROP FUNCTION IF EXISTS resolve_reel_coverage(uuid);

DROP TABLE IF EXISTS client_events;
DROP TABLE IF EXISTS user_blocks;
DROP TABLE IF EXISTS reel_counters;
DROP TABLE IF EXISTS reel_reports;
DROP TABLE IF EXISTS author_trust;
DROP TABLE IF EXISTS moderation_decisions;
DROP TABLE IF EXISTS moderation_labels;
DROP TABLE IF EXISTS moderation_jobs;
DROP TABLE IF EXISTS reel_tags;
DROP TABLE IF EXISTS reel_state_history;
DROP TABLE IF EXISTS reel_renditions;
DROP TABLE IF EXISTS reels;
DROP TABLE IF EXISTS media_assets;
DROP TABLE IF EXISTS platform_admins;
DROP TABLE IF EXISTS vendor_users;

DROP TYPE IF EXISTS reel_state;
