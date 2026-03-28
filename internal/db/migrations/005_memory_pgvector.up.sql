-- Enable pgvector extension for embedding storage.
CREATE EXTENSION IF NOT EXISTS vector;

-- Long-term memory entries with vector embeddings.
CREATE TABLE memories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'fact',  -- fact, preference, learned, entity, observation, reflection
    importance  DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    embedding   vector(1536),  -- text-embedding-3-small dimension
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memories_tenant_user ON memories(tenant_id, user_id);
CREATE INDEX idx_memories_type ON memories(tenant_id, type);

-- HNSW index for fast approximate nearest neighbor search.
CREATE INDEX idx_memories_embedding ON memories USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
