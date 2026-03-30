-- Dynamic LLM provider configuration — admin-managed at runtime.
CREATE TABLE llm_providers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,         -- display name: "OpenAI", "Anthropic", "Local LLaMA"
    type        TEXT NOT NULL,                -- provider type: "openai", "anthropic", "ollama", "openai-compatible"
    api_key     TEXT,                         -- encrypted at rest (via credential_key)
    base_url    TEXT,                         -- custom endpoint (Azure, LM Studio, vLLM, etc.)
    config      JSONB NOT NULL DEFAULT '{}',  -- provider-specific config (org_id, api_version, headers, etc.)
    enabled     BOOLEAN NOT NULL DEFAULT true,
    priority    INTEGER NOT NULL DEFAULT 0,   -- higher = preferred. Used for default selection.
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Per-tenant model restrictions (optional). If no rows exist for a tenant, all models are available.
CREATE TABLE tenant_llm_config (
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider_id     UUID NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
    allowed_models  JSONB,   -- null = all models allowed, ["gpt-4o", "gpt-4o-mini"] = specific models
    PRIMARY KEY (tenant_id, provider_id)
);

CREATE INDEX idx_llm_providers_enabled ON llm_providers(enabled);
CREATE INDEX idx_tenant_llm_config_tenant ON tenant_llm_config(tenant_id);
