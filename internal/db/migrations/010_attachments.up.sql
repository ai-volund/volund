-- Conversation file attachments.
-- Files are stored in object storage (S3/MinIO/local); this table tracks metadata.
CREATE TABLE attachments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    user_id         UUID,
    file_name       TEXT NOT NULL,
    mime_type       TEXT NOT NULL,
    size            BIGINT NOT NULL,             -- bytes
    storage_key     TEXT NOT NULL,                -- object storage path
    metadata        JSONB NOT NULL DEFAULT '{}',  -- dimensions, duration, page count, etc.
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_attachments_conversation ON attachments(conversation_id);
CREATE INDEX idx_attachments_tenant ON attachments(tenant_id);
