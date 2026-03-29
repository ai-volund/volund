-- Provider definitions now come from skill manifests.
-- client_id/secret are nullable — empty until admin configures them.
-- skill_id tracks which skill registered the provider.
-- configured is true when admin has supplied credentials.

ALTER TABLE oauth_providers
    ALTER COLUMN client_id SET DEFAULT '',
    ALTER COLUMN client_secret SET DEFAULT '',
    ADD COLUMN IF NOT EXISTS skill_id    TEXT,
    ADD COLUMN IF NOT EXISTS configured  BOOLEAN NOT NULL DEFAULT false;

-- Allow empty client_id/secret (providers registered by skills start unconfigured)
ALTER TABLE oauth_providers
    ALTER COLUMN client_id DROP NOT NULL,
    ALTER COLUMN client_secret DROP NOT NULL;
