-- Per-user skill enablement. Users can only enable skills that are
-- installed at the tenant level (tenant_skills table from migration 003).

CREATE TABLE user_skills (
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    skill_id    UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id, skill_id)
);

CREATE INDEX idx_user_skills_user ON user_skills(tenant_id, user_id);
