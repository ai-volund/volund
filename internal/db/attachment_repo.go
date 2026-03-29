package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Attachment represents a file uploaded to a conversation.
type Attachment struct {
	ID             string
	ConversationID string
	TenantID       string
	UserID         *string
	FileName       string
	MimeType       string
	Size           int64
	StorageKey     string
	Metadata       map[string]any
	CreatedAt      time.Time
}

// AttachmentRepo handles attachment persistence.
type AttachmentRepo struct{ pool *Pool }

// NewAttachmentRepo creates an AttachmentRepo.
func NewAttachmentRepo(pool *Pool) *AttachmentRepo { return &AttachmentRepo{pool: pool} }

// Create inserts a new attachment record.
func (r *AttachmentRepo) Create(ctx context.Context, a *Attachment) (*Attachment, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO attachments (conversation_id, tenant_id, user_id, file_name, mime_type, size, storage_key, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, conversation_id, tenant_id, user_id, file_name, mime_type, size, storage_key, metadata, created_at
	`, a.ConversationID, a.TenantID, a.UserID, a.FileName, a.MimeType, a.Size, a.StorageKey, a.Metadata)
	return scanAttachment(row)
}

// Get retrieves a single attachment by ID.
func (r *AttachmentRepo) Get(ctx context.Context, id string) (*Attachment, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, conversation_id, tenant_id, user_id, file_name, mime_type, size, storage_key, metadata, created_at
		FROM attachments WHERE id = $1
	`, id)
	return scanAttachment(row)
}

// ListByConversation returns all attachments for a conversation, ordered by creation time.
func (r *AttachmentRepo) ListByConversation(ctx context.Context, conversationID string) ([]*Attachment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, conversation_id, tenant_id, user_id, file_name, mime_type, size, storage_key, metadata, created_at
		FROM attachments
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Attachment, error) {
		return scanAttachment(row)
	})
}

// Delete removes an attachment by ID.
func (r *AttachmentRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id)
	return err
}

func scanAttachment(row pgx.Row) (*Attachment, error) {
	a := &Attachment{}
	err := row.Scan(
		&a.ID, &a.ConversationID, &a.TenantID, &a.UserID,
		&a.FileName, &a.MimeType, &a.Size, &a.StorageKey, &a.Metadata, &a.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan attachment: %w", err)
	}
	return a, nil
}
