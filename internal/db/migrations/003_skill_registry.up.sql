-- Skill registry — the Forge catalog of available skills.
-- Skills can be published by anyone and installed by tenants.
CREATE TABLE skills (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    version         TEXT NOT NULL DEFAULT '0.0.0',
    description     TEXT NOT NULL DEFAULT '',
    author          TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL DEFAULT 'prompt',  -- 'prompt', 'mcp', 'cli'
    tags            TEXT[] NOT NULL DEFAULT '{}',
    spec            JSONB NOT NULL DEFAULT '{}',     -- full skill spec (prompt, runtime, cli config)
    readme          TEXT NOT NULL DEFAULT '',         -- rendered markdown readme
    downloads       BIGINT NOT NULL DEFAULT 0,
    published       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_skills_type ON skills(type);
CREATE INDEX idx_skills_tags ON skills USING gin(tags);
CREATE INDEX idx_skills_author ON skills(author);

-- Tenant skill installs — tracks which skills a tenant has enabled.
CREATE TABLE tenant_skills (
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id        UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    installed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, skill_id)
);
