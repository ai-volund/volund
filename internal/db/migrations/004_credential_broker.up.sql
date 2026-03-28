-- Credential broker: per-user, per-tenant credential storage with audit trail.

-- User credentials (encrypted OAuth tokens, API keys, etc.)
CREATE TABLE user_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,           -- 'github', 'slack', 'jira', etc.
    encrypted_token BYTEA NOT NULL,          -- AES-256-GCM encrypted token
    scopes          TEXT[] NOT NULL DEFAULT '{}',
    token_type      TEXT NOT NULL DEFAULT 'oauth2',  -- 'oauth2', 'api_key', 'pat'
    expires_at      TIMESTAMPTZ,             -- NULL if non-expiring (e.g., PAT)
    refresh_token   BYTEA,                   -- encrypted refresh token (OAuth2)
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, user_id, provider)
);

-- Credential access audit log
CREATE TABLE credential_audit_log (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL,       -- authorization identity (whose credential)
    provider            TEXT NOT NULL,
    agent_instance_id   TEXT,                -- execution identity (which agent pod)
    conversation_id     UUID,                -- context
    skill_name          TEXT,                -- which skill requested the credential
    action              TEXT NOT NULL,        -- 'token_issued', 'token_denied', 'credential_stored', 'credential_deleted'
    scopes_requested    TEXT[],
    scopes_granted      TEXT[],
    ip_address          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_credentials_tenant ON user_credentials(tenant_id);
CREATE INDEX idx_user_credentials_user ON user_credentials(tenant_id, user_id);
CREATE INDEX idx_user_credentials_provider ON user_credentials(tenant_id, provider);
CREATE INDEX idx_credential_audit_tenant ON credential_audit_log(tenant_id);
CREATE INDEX idx_credential_audit_user ON credential_audit_log(tenant_id, user_id);
CREATE INDEX idx_credential_audit_created ON credential_audit_log(created_at);
