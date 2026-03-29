ALTER TABLE oauth_providers
    DROP COLUMN IF EXISTS skill_id,
    DROP COLUMN IF EXISTS configured;

ALTER TABLE oauth_providers
    ALTER COLUMN client_id SET NOT NULL,
    ALTER COLUMN client_secret SET NOT NULL;
