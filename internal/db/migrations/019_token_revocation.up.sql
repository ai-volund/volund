-- Token revocation list — JWTs added here are rejected by the gateway.
-- TTL matches the JWT expiration (15m), so entries auto-expire.
CREATE TABLE IF NOT EXISTS revoked_tokens (
    jti         TEXT PRIMARY KEY,       -- JWT ID (or user_id for blanket revocation)
    reason      TEXT,
    revoked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL    -- when the JWT would have expired anyway
);

CREATE INDEX idx_revoked_tokens_expires ON revoked_tokens (expires_at);
