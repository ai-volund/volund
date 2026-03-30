-- Agent teams — groups of agents with an orchestrator.
CREATE TABLE IF NOT EXISTS teams (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    description             TEXT DEFAULT '',
    orchestrator_profile_id UUID REFERENCES agent_profiles(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

-- Team members — which agent profiles belong to a team.
CREATE TABLE IF NOT EXISTS team_members (
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    profile_id  UUID NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member',  -- 'orchestrator', 'member'
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (team_id, profile_id)
);

CREATE INDEX idx_teams_tenant ON teams (tenant_id);
CREATE INDEX idx_team_members_team ON team_members (team_id);
