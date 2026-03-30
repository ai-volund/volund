-- User notifications — task completions, agent updates, system messages.
CREATE TABLE IF NOT EXISTS notifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,          -- "task_complete", "task_failed", "agent_message", "system"
    title       TEXT NOT NULL,
    body        TEXT,
    read        BOOLEAN NOT NULL DEFAULT false,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notifications_user ON notifications (user_id, read, created_at DESC);
