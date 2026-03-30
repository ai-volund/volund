DROP INDEX IF EXISTS idx_usage_events_user;
ALTER TABLE usage_events DROP COLUMN IF EXISTS user_id;
