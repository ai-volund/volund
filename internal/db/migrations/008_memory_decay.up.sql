-- Add decay tracking columns to memories table.
ALTER TABLE memories
    ADD COLUMN IF NOT EXISTS access_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_accessed TIMESTAMPTZ;

-- Index for decay-based queries (frequently accessed, recently accessed).
CREATE INDEX IF NOT EXISTS idx_memories_last_accessed
    ON memories (tenant_id, last_accessed DESC NULLS LAST);
