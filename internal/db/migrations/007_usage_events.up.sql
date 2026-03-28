CREATE TABLE IF NOT EXISTS usage_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    task_id         TEXT,
    instance_id     TEXT,
    provider        TEXT NOT NULL,
    model           TEXT NOT NULL,
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER GENERATED ALWAYS AS (input_tokens + output_tokens) STORED,
    cost_usd        NUMERIC(10, 6),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_usage_events_tenant ON usage_events (tenant_id, created_at DESC);
CREATE INDEX idx_usage_events_conv ON usage_events (conversation_id) WHERE conversation_id IS NOT NULL;
