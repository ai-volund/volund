-- Tenant quota configuration and budget tracking.
-- Supports multi-dimensional quotas: tokens, cost, and compute.

CREATE TABLE tenant_quotas (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Token limits (per billing period).
    max_tokens      BIGINT,          -- NULL = unlimited
    max_cost_usd    NUMERIC(10, 2),  -- NULL = unlimited

    -- Billing period (monthly by default).
    period_start    TIMESTAMPTZ NOT NULL DEFAULT date_trunc('month', NOW()),
    period_end      TIMESTAMPTZ NOT NULL DEFAULT date_trunc('month', NOW()) + INTERVAL '1 month',

    -- Budget enforcement action: 'alert', 'queue', 'block'
    on_limit_action TEXT NOT NULL DEFAULT 'alert',

    -- Alert webhook URL (called at 80%, 90%, 100% thresholds).
    alert_webhook_url TEXT,

    -- Tracking: which thresholds have been fired this period.
    alerted_80      BOOLEAN NOT NULL DEFAULT false,
    alerted_90      BOOLEAN NOT NULL DEFAULT false,
    alerted_100     BOOLEAN NOT NULL DEFAULT false,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id)
);

CREATE INDEX idx_tenant_quotas_tenant ON tenant_quotas (tenant_id);

-- Down migration: DROP TABLE tenant_quotas;
