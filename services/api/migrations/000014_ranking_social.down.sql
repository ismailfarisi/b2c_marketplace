-- Reverse of 000014. Dependents before dependencies.

DROP TABLE IF EXISTS ranking_profiles;

DROP INDEX IF EXISTS client_events_viewer_idx;
ALTER TABLE client_events DROP COLUMN IF EXISTS viewer_key;
ALTER TABLE client_events DROP COLUMN IF EXISTS watched_ms;
ALTER TABLE client_events DROP COLUMN IF EXISTS completed;

DROP TABLE IF EXISTS viewer_interests;
DROP TABLE IF EXISTS author_quality;
DROP TABLE IF EXISTS reel_features;

DROP TABLE IF EXISTS comment_reports;

DROP TRIGGER IF EXISTS reel_comments_depth ON reel_comments;
DROP TRIGGER IF EXISTS reel_comments_set_updated_at ON reel_comments;
DROP TABLE IF EXISTS reel_comments;
DROP FUNCTION IF EXISTS reel_comments_set_depth();

DROP TABLE IF EXISTS reel_likes;
