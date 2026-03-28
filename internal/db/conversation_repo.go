package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Conversation is a chat thread.
type Conversation struct {
	ID        string
	TenantID  string
	UserID    *string
	Title     string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message is a single message in a conversation.
type Message struct {
	ID             string
	ConversationID string
	Role           string
	AuthorType     string
	AuthorID       *string
	AgentName      *string
	Content        json.RawMessage
	ToolCallID     *string
	InputTokens    int
	OutputTokens   int
	CreatedAt      time.Time
}

// ConversationRepo handles conversation and message persistence.
type ConversationRepo struct{ pool *Pool }

// NewConversationRepo creates a ConversationRepo.
func NewConversationRepo(pool *Pool) *ConversationRepo { return &ConversationRepo{pool: pool} }

// ── Conversations ─────────────────────────────────────────────────────────────

func (r *ConversationRepo) Create(ctx context.Context, tenantID string, userID *string, title string) (*Conversation, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO conversations (tenant_id, user_id, title)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, user_id, title, status, created_at, updated_at
	`, tenantID, userID, title)
	return scanConversation(row)
}

func (r *ConversationRepo) Get(ctx context.Context, id string) (*Conversation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, title, status, created_at, updated_at
		FROM conversations WHERE id = $1
	`, id)
	return scanConversation(row)
}

func (r *ConversationRepo) ListByUser(ctx context.Context, tenantID, userID string) ([]*Conversation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, title, status, created_at, updated_at
		FROM conversations
		WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'
		ORDER BY updated_at DESC
	`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Conversation, error) {
		return scanConversation(row)
	})
}

func (r *ConversationRepo) UpdateTitle(ctx context.Context, id, title string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE conversations SET title = $2, updated_at = NOW() WHERE id = $1
	`, id, title)
	return err
}

func (r *ConversationRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE conversations SET status = 'deleted', updated_at = NOW() WHERE id = $1`, id)
	return err
}

// ── Messages ──────────────────────────────────────────────────────────────────

func (r *ConversationRepo) AddMessage(ctx context.Context, m *Message) (*Message, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO messages
		  (conversation_id, role, author_type, author_id, agent_name, content,
		   tool_call_id, input_tokens, output_tokens)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, conversation_id, role, author_type, author_id, agent_name,
		          content, tool_call_id, input_tokens, output_tokens, created_at
	`, m.ConversationID, m.Role, m.AuthorType, m.AuthorID, m.AgentName,
		m.Content, m.ToolCallID, m.InputTokens, m.OutputTokens)

	// Touch the conversation's updated_at.
	_, _ = r.pool.Exec(ctx, `
		UPDATE conversations SET updated_at = NOW() WHERE id = $1
	`, m.ConversationID)

	return scanMessage(row)
}

func (r *ConversationRepo) ListMessages(ctx context.Context, conversationID string, limit int) ([]*Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, conversation_id, role, author_type, author_id, agent_name,
		       content, tool_call_id, input_tokens, output_tokens, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
		LIMIT $2
	`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Message, error) {
		return scanMessage(row)
	})
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

func scanConversation(row pgx.Row) (*Conversation, error) {
	c := &Conversation{}
	err := row.Scan(&c.ID, &c.TenantID, &c.UserID, &c.Title, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	return c, nil
}

func scanMessage(row pgx.Row) (*Message, error) {
	m := &Message{}
	err := row.Scan(
		&m.ID, &m.ConversationID, &m.Role, &m.AuthorType, &m.AuthorID,
		&m.AgentName, &m.Content, &m.ToolCallID, &m.InputTokens, &m.OutputTokens,
		&m.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}
	return m, nil
}
