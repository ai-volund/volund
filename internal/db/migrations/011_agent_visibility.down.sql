DROP INDEX IF EXISTS idx_agent_profiles_owner;
DROP INDEX IF EXISTS idx_agent_profiles_visibility;
ALTER TABLE agent_profiles DROP CONSTRAINT IF EXISTS chk_user_visibility_owner;
ALTER TABLE agent_profiles DROP COLUMN IF EXISTS owner_id;
ALTER TABLE agent_profiles DROP COLUMN IF EXISTS visibility;
