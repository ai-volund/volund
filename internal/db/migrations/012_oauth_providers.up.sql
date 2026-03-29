-- Generic OAuth2 provider registry. Admins register providers at runtime
-- so new OAuth2 services can be added without recompiling the application.

CREATE TABLE oauth_providers (
    id               TEXT PRIMARY KEY,                -- e.g. "gmail", "slack", "notion"
    display_name     TEXT NOT NULL,                   -- human-readable name
    auth_url         TEXT NOT NULL,                   -- authorization endpoint
    token_url        TEXT NOT NULL,                   -- token exchange endpoint
    client_id        TEXT NOT NULL,                   -- OAuth2 client ID
    client_secret    TEXT NOT NULL,                   -- OAuth2 client secret (encrypted at rest by disk)
    scopes           TEXT[] NOT NULL DEFAULT '{}',    -- default scopes to request
    redirect_path    TEXT,                            -- callback path override (default: /v1/connect/{id}/callback)
    extra_auth_params JSONB,                          -- provider-specific auth params (e.g. {"access_type":"offline"})
    extra_headers    JSONB,                           -- extra headers for token request
    icon_url         TEXT,                            -- provider icon for UI
    category         TEXT,                            -- "email", "calendar", "messaging", etc.
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
