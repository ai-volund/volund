-- Scheduled workflows — cron-based agent execution.
CREATE TABLE IF NOT EXISTS schedules (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    cron_expression   TEXT NOT NULL,           -- standard cron: "0 6 * * *" = daily at 6am
    agent_profile_id  UUID REFERENCES agent_profiles(id) ON DELETE SET NULL,
    prompt            TEXT NOT NULL,           -- the message to send to the agent
    delivery_method   TEXT NOT NULL DEFAULT 'conversation',  -- conversation, email, webhook
    delivery_config   JSONB DEFAULT '{}',      -- { "email": "user@example.com" } or { "webhook_url": "..." }
    enabled           BOOLEAN NOT NULL DEFAULT true,
    last_run_at       TIMESTAMPTZ,
    next_run_at       TIMESTAMPTZ,
    last_run_status   TEXT,                    -- "success", "failed", "running"
    last_run_error    TEXT,
    run_count         INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_schedules_tenant ON schedules (tenant_id);
CREATE INDEX idx_schedules_next_run ON schedules (next_run_at) WHERE enabled = true;
