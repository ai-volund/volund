-- General audit log for all state-changing API actions.
CREATE TABLE audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT,
    user_id     TEXT,
    action      TEXT NOT NULL,       -- "POST /v1/admin/agents", "DELETE /v1/tenants/xxx"
    resource    TEXT,                -- resource type: "agent", "tenant", "skill", "llm_provider"
    resource_id TEXT,                -- ID of the affected resource
    detail      JSONB DEFAULT '{}',  -- request body summary, extra context
    ip_address  TEXT,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_tenant ON audit_log(tenant_id);
CREATE INDEX idx_audit_log_created ON audit_log(created_at);
CREATE INDEX idx_audit_log_action ON audit_log(action);
