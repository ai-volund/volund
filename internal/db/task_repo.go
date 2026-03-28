package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Task is an async agent job.
type Task struct {
	ID             string
	TenantID       string
	ConversationID *string
	AgentID        *string
	Title          string
	Status         string // pending, running, completed, failed, cancelled
	Result         json.RawMessage
	Error          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

// TaskRepo handles task persistence.
type TaskRepo struct{ pool *Pool }

// NewTaskRepo creates a TaskRepo.
func NewTaskRepo(pool *Pool) *TaskRepo { return &TaskRepo{pool: pool} }

func (r *TaskRepo) Create(ctx context.Context, tenantID string, conversationID *string, title string) (*Task, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tasks (tenant_id, conversation_id, title)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, conversation_id, agent_id, title, status,
		          result, error, created_at, updated_at, completed_at
	`, tenantID, conversationID, title)
	return scanTask(row)
}

func (r *TaskRepo) Get(ctx context.Context, id string) (*Task, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, conversation_id, agent_id, title, status,
		       result, error, created_at, updated_at, completed_at
		FROM tasks WHERE id = $1
	`, id)
	return scanTask(row)
}

func (r *TaskRepo) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, conversation_id, agent_id, title, status,
		       result, error, created_at, updated_at, completed_at
		FROM tasks WHERE tenant_id = $1
		ORDER BY created_at DESC LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Task, error) {
		return scanTask(row)
	})
}

func (r *TaskRepo) SetRunning(ctx context.Context, id, agentID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tasks SET status = 'running', agent_id = $2, updated_at = NOW()
		WHERE id = $1
	`, id, agentID)
	return err
}

func (r *TaskRepo) Complete(ctx context.Context, id string, result json.RawMessage) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tasks
		SET status = 'completed', result = $2, updated_at = NOW(), completed_at = NOW()
		WHERE id = $1
	`, id, result)
	return err
}

func (r *TaskRepo) Fail(ctx context.Context, id, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tasks
		SET status = 'failed', error = $2, updated_at = NOW(), completed_at = NOW()
		WHERE id = $1
	`, id, errMsg)
	return err
}

func (r *TaskRepo) Cancel(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tasks SET status = 'cancelled', updated_at = NOW() WHERE id = $1
	`, id)
	return err
}

func scanTask(row pgx.Row) (*Task, error) {
	t := &Task{}
	err := row.Scan(
		&t.ID, &t.TenantID, &t.ConversationID, &t.AgentID, &t.Title, &t.Status,
		&t.Result, &t.Error, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	return t, nil
}
