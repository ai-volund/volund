-- Add user_id to usage_events for per-user usage tracking.
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_usage_events_user ON usage_events (user_id) WHERE user_id IS NOT NULL;
