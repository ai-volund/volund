-- Add visibility and owner_id columns to agent_profiles for two-tier agent model.
-- System agents (visibility='system') are visible to all users in a tenant.
-- User agents (visibility='user') are only visible to the owner.

ALTER TABLE agent_profiles
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'system',
    ADD COLUMN owner_id   TEXT;

-- Ensure user-scoped profiles have a non-null owner_id.
ALTER TABLE agent_profiles
    ADD CONSTRAINT chk_user_visibility_owner
    CHECK (visibility != 'user' OR owner_id IS NOT NULL);

-- Index for listing user-visible profiles efficiently.
CREATE INDEX idx_agent_profiles_visibility ON agent_profiles (tenant_id, visibility);
CREATE INDEX idx_agent_profiles_owner ON agent_profiles (tenant_id, owner_id) WHERE owner_id IS NOT NULL;
