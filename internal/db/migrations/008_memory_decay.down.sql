DROP INDEX IF EXISTS idx_memories_last_accessed;
ALTER TABLE memories DROP COLUMN IF EXISTS last_accessed;
ALTER TABLE memories DROP COLUMN IF EXISTS access_count;
